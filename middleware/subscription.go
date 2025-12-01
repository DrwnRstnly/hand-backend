package middleware

import (
	"net/http"

	"github.com/Hand-TBN1/hand-backend/apierror"
	"github.com/Hand-TBN1/hand-backend/models"
	"github.com/Hand-TBN1/hand-backend/services"
	"github.com/Hand-TBN1/hand-backend/utilities"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// PremiumOnly ensures the caller has an active premium subscription.
// Attach this middleware to premium-only routes (e.g., Emergency or Medical Report APIs).
func PremiumOnly(db *gorm.DB) gin.HandlerFunc {
	subscriptionService := &services.SubscriptionService{DB: db}

	return func(c *gin.Context) {
		claimsValue, ok := c.Get("claims")
		if !ok {
			c.JSON(http.StatusUnauthorized, apierror.NewApiErrorBuilder().
				WithStatus(http.StatusUnauthorized).
				WithMessage("Unauthorized access").
				Build())
			c.Abort()
			return
		}

		userClaims, ok := claimsValue.(*utilities.Claims)
		if !ok {
			c.JSON(http.StatusUnauthorized, apierror.NewApiErrorBuilder().
				WithStatus(http.StatusUnauthorized).
				WithMessage("Unauthorized access").
				Build())
			c.Abort()
			return
		}

		subscription, err := subscriptionService.GetSubscriptionForUser(userClaims.UserID)
		if err != nil || subscription.Plan != models.SubscriptionPlanPremium || subscription.Status != models.SubscriptionStatusActive {
			c.JSON(http.StatusForbidden, apierror.NewApiErrorBuilder().
				WithStatus(http.StatusForbidden).
				WithMessage("Premium subscription required").
				Build())
			c.Abort()
			return
		}

		c.Next()
	}
}
