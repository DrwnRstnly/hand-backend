package controller

import (
	"net/http"

	"github.com/Hand-TBN1/hand-backend/apierror"
	"github.com/Hand-TBN1/hand-backend/services"
	"github.com/Hand-TBN1/hand-backend/utilities"
	"github.com/gin-gonic/gin"
)

type SubscriptionController struct {
	SubscriptionService *services.SubscriptionService
}

func (ctrl *SubscriptionController) ListPlans(c *gin.Context) {
	plans := ctrl.SubscriptionService.GetPlans()
	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

func (ctrl *SubscriptionController) GetMySubscription(c *gin.Context) {
	claims, exists := c.Get("claims")
	if !exists {
		c.JSON(http.StatusUnauthorized, apierror.NewApiErrorBuilder().
			WithStatus(http.StatusUnauthorized).
			WithMessage("Unauthorized access").
			Build())
		return
	}
	userClaims := claims.(*utilities.Claims)

	status, err := ctrl.SubscriptionService.GetSubscriptionForUser(userClaims.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, apierror.NewApiErrorBuilder().
			WithStatus(http.StatusInternalServerError).
			WithMessage(err.Error()).
			Build())
		return
	}

	c.JSON(http.StatusOK, gin.H{"subscription": status})
}

func (ctrl *SubscriptionController) CreatePremiumCheckout(c *gin.Context) {
	claims, exists := c.Get("claims")
	if !exists {
		c.JSON(http.StatusUnauthorized, apierror.NewApiErrorBuilder().
			WithStatus(http.StatusUnauthorized).
			WithMessage("Unauthorized access").
			Build())
		return
	}
	userClaims := claims.(*utilities.Claims)

	resp, err := ctrl.SubscriptionService.CreatePremiumCheckout(userClaims.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, apierror.NewApiErrorBuilder().
			WithStatus(http.StatusBadRequest).
			WithMessage(err.Error()).
			Build())
		return
	}

	c.JSON(http.StatusOK, resp)
}

// HandlePaymentNotification processes Midtrans webhook for subscriptions.
func (ctrl *SubscriptionController) HandlePaymentNotification(c *gin.Context) {
	var notification map[string]interface{}
	if err := c.ShouldBindJSON(&notification); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid notification payload"})
		return
	}

	orderID, ok := notification["order_id"].(string)
	if !ok || orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_id missing"})
		return
	}
	transactionStatus, ok := notification["transaction_status"].(string)
	if !ok || transactionStatus == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "transaction_status missing"})
		return
	}

	if err := ctrl.SubscriptionService.HandlePaymentNotification(orderID, transactionStatus); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Subscription payment status updated"})
}
