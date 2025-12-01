package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Hand-TBN1/hand-backend/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	premiumMonthlyPrice int64 = 20000
	freeChatLimitPerDay       = 30
)

type PlanView struct {
	Name            string                  `json:"name"`
	Plan            models.SubscriptionPlan `json:"plan"`
	Price           int64                   `json:"price"`
	Currency        string                  `json:"currency"`
	BillingInterval string                  `json:"billing_interval"`
	Features        []string                `json:"features"`
	ChatLimitPerDay *int                    `json:"chat_limit_per_day,omitempty"`
}

type SubscriptionStatusView struct {
	Plan               models.SubscriptionPlan   `json:"plan"`
	Status             models.SubscriptionStatus `json:"status"`
	Price              int64                     `json:"price"`
	OrderID            string                    `json:"order_id,omitempty"`
	PaymentToken       string                    `json:"payment_token,omitempty"`
	PaymentRedirectURL string                    `json:"payment_redirect_url,omitempty"`
	StartsAt           *time.Time                `json:"starts_at,omitempty"`
	ExpiresAt          *time.Time                `json:"expires_at,omitempty"`
	ChatLimitPerDay    *int                      `json:"chat_limit_per_day,omitempty"`
}

type CheckoutResponse struct {
	OrderID        string `json:"order_id"`
	SubscriptionID string `json:"subscription_id"`
	Token          string `json:"token"`
	RedirectURL    string `json:"redirect_url"`
}

type SubscriptionService struct {
	DB             *gorm.DB
	PaymentService *PaymentService
}

// GetPlans returns the static list of plans with their features.
func (s *SubscriptionService) GetPlans() []PlanView {
	freeLimit := freeChatLimitPerDay
	return []PlanView{
		{
			Name:            "Free",
			Plan:            models.SubscriptionPlanFree,
			Price:           0,
			Currency:        "IDR",
			BillingInterval: "monthly",
			Features: []string{
				"Dashboard",
				"Journal",
				"Health Plan",
				"Mood Tracker",
				"Real Time Chat (limited)",
			},
			ChatLimitPerDay: &freeLimit,
		},
		{
			Name:            "Premium",
			Plan:            models.SubscriptionPlanPremium,
			Price:           premiumMonthlyPrice,
			Currency:        "IDR",
			BillingInterval: "monthly",
			Features: []string{
				"Dashboard",
				"Journal",
				"Health Plan",
				"Mood Tracker",
				"Real Time Chat (unlimited)",
				"Emergency",
				"Medical Report",
			},
		},
	}
}

// GetSubscriptionForUser fetches the latest subscription information or falls back to free plan.
func (s *SubscriptionService) GetSubscriptionForUser(userID string) (*SubscriptionStatusView, error) {
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id")
	}

	if err := s.expireIfNeeded(userUUID); err != nil {
		return nil, err
	}

	sub, err := s.getLatestSubscription(userUUID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if sub == nil {
		limit := freeChatLimitPerDay
		return &SubscriptionStatusView{
			Plan:            models.SubscriptionPlanFree,
			Status:          models.SubscriptionStatusActive,
			Price:           0,
			ChatLimitPerDay: &limit,
		}, nil
	}

	effectivePlan := sub.Plan
	if sub.Plan == models.SubscriptionPlanPremium && sub.Status != models.SubscriptionStatusActive {
		effectivePlan = models.SubscriptionPlanFree
	}

	var chatLimit *int
	if effectivePlan == models.SubscriptionPlanFree {
		limit := freeChatLimitPerDay
		chatLimit = &limit
	}

	return &SubscriptionStatusView{
		Plan:               effectivePlan,
		Status:             sub.Status,
		Price:              priceForPlan(effectivePlan, sub.Price),
		OrderID:            sub.OrderID,
		PaymentToken:       sub.PaymentToken,
		PaymentRedirectURL: sub.PaymentRedirectURL,
		StartsAt:           sub.StartsAt,
		ExpiresAt:          sub.ExpiresAt,
		ChatLimitPerDay:    chatLimit,
	}, nil
}

// CreatePremiumCheckout starts a payment for a monthly premium plan.
func (s *SubscriptionService) CreatePremiumCheckout(userID string) (*CheckoutResponse, error) {
	if s.PaymentService == nil {
		return nil, fmt.Errorf("payment service not configured")
	}

	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id")
	}

	active, err := s.getActiveSubscription(userUUID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if active != nil && active.Plan == models.SubscriptionPlanPremium {
		return nil, fmt.Errorf("premium plan already active until %s", active.ExpiresAt.Format(time.RFC3339))
	}

	pending, err := s.findLatestSubscriptionByStatus(userUUID, models.SubscriptionStatusPending)
	if err == nil && pending != nil && pending.OrderID != "" && pending.PaymentToken != "" {
		return &CheckoutResponse{
			OrderID:        pending.OrderID,
			SubscriptionID: pending.ID.String(),
			Token:          pending.PaymentToken,
			RedirectURL:    pending.PaymentRedirectURL,
		}, nil
	}

	orderID := generateSubscriptionOrderID(userUUID)
	subscription := models.Subscription{
		UserID:  userUUID,
		Plan:    models.SubscriptionPlanPremium,
		Status:  models.SubscriptionStatusPending,
		OrderID: orderID,
		Price:   premiumMonthlyPrice,
	}

	if err := s.DB.Create(&subscription).Error; err != nil {
		return nil, err
	}

	paymentResp, err := s.PaymentService.CreatePaymentWithCallback(orderID, premiumMonthlyPrice, "https://hand.tbn1.site/subscription")
	if err != nil {
		return nil, err
	}

	subscription.PaymentToken = paymentResp.Token
	subscription.PaymentRedirectURL = paymentResp.RedirectURL
	if err := s.DB.Save(&subscription).Error; err != nil {
		return nil, err
	}

	return &CheckoutResponse{
		OrderID:        orderID,
		SubscriptionID: subscription.ID.String(),
		Token:          subscription.PaymentToken,
		RedirectURL:    subscription.PaymentRedirectURL,
	}, nil
}

// HandlePaymentNotification updates subscription status based on Midtrans webhook.
func (s *SubscriptionService) HandlePaymentNotification(orderID, transactionStatus string) error {
	var subscription models.Subscription
	if err := s.DB.Where("order_id = ?", orderID).First(&subscription).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("subscription not found for order: %s", orderID)
		}
		return err
	}

	now := time.Now()
	lowerStatus := strings.ToLower(transactionStatus)
	switch lowerStatus {
	case "capture", "settlement", "success":
		start := now
		expires := now.AddDate(0, 1, 0)
		subscription.Status = models.SubscriptionStatusActive
		subscription.StartsAt = &start
		subscription.ExpiresAt = &expires
		subscription.Plan = models.SubscriptionPlanPremium
	case "pending", "challenge":
		subscription.Status = models.SubscriptionStatusPending
	case "deny", "cancel", "expire", "failure":
		subscription.Status = models.SubscriptionStatusCanceled
	default:
		return fmt.Errorf("unsupported transaction status: %s", transactionStatus)
	}

	return s.DB.Save(&subscription).Error
}

func (s *SubscriptionService) getActiveSubscription(userID uuid.UUID) (*models.Subscription, error) {
	now := time.Now()
	var subscription models.Subscription
	if err := s.DB.
		Where("user_id = ? AND status = ? AND expires_at IS NOT NULL AND expires_at > ?", userID, models.SubscriptionStatusActive, now).
		Order("expires_at desc").
		First(&subscription).Error; err != nil {
		return nil, err
	}
	return &subscription, nil
}

func (s *SubscriptionService) getLatestSubscription(userID uuid.UUID) (*models.Subscription, error) {
	var subscription models.Subscription
	if err := s.DB.
		Where("user_id = ?", userID).
		Order("created_at desc").
		First(&subscription).Error; err != nil {
		return nil, err
	}
	return &subscription, nil
}

func (s *SubscriptionService) findLatestSubscriptionByStatus(userID uuid.UUID, status models.SubscriptionStatus) (*models.Subscription, error) {
	var subscription models.Subscription
	if err := s.DB.
		Where("user_id = ? AND status = ?", userID, status).
		Order("created_at desc").
		First(&subscription).Error; err != nil {
		return nil, err
	}
	return &subscription, nil
}

func (s *SubscriptionService) expireIfNeeded(userID uuid.UUID) error {
	now := time.Now()
	result := s.DB.Model(&models.Subscription{}).
		Where("user_id = ? AND status = ? AND expires_at IS NOT NULL AND expires_at <= ?", userID, models.SubscriptionStatusActive, now).
		Update("status", models.SubscriptionStatusExpired)
	return result.Error
}

func generateSubscriptionOrderID(userID uuid.UUID) string {
	stripped := strings.ReplaceAll(userID.String(), "-", "")
	timestamp := time.Now().Unix()
	return fmt.Sprintf("sub-%d-%s", timestamp, stripped)
}

func priceForPlan(plan models.SubscriptionPlan, storedPrice int64) int64 {
	if plan == models.SubscriptionPlanPremium {
		if storedPrice > 0 {
			return storedPrice
		}
		return premiumMonthlyPrice
	}
	return 0
}
