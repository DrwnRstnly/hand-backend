package models

import (
	"time"

	"github.com/google/uuid"
)

// SubscriptionPlan represents the type of subscription a user has.
type SubscriptionPlan string

// SubscriptionStatus captures the lifecycle of a subscription.
type SubscriptionStatus string

const (
	SubscriptionPlanFree    SubscriptionPlan = "free"
	SubscriptionPlanPremium SubscriptionPlan = "premium"
)

const (
	SubscriptionStatusPending  SubscriptionStatus = "pending"
	SubscriptionStatusActive   SubscriptionStatus = "active"
	SubscriptionStatusExpired  SubscriptionStatus = "expired"
	SubscriptionStatusCanceled SubscriptionStatus = "canceled"
)

// Subscription tracks the paid tier for a user.
type Subscription struct {
	ID                 uuid.UUID          `gorm:"type:uuid;default:uuid_generate_v4();primary_key"`
	UserID             uuid.UUID          `gorm:"type:uuid;not null;index"`
	Plan               SubscriptionPlan   `gorm:"type:subscription_plan_enum;not null"`
	Status             SubscriptionStatus `gorm:"type:subscription_status_enum;not null"`
	OrderID            string             `gorm:"uniqueIndex"`
	Price              int64
	PaymentToken       string
	PaymentRedirectURL string
	StartsAt           *time.Time
	ExpiresAt          *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time

	// Associations
	User User `gorm:"foreignKey:UserID"`
}
