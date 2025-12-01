package routes

import (
	"github.com/Hand-TBN1/hand-backend/controller"
	"github.com/Hand-TBN1/hand-backend/middleware"
	"github.com/Hand-TBN1/hand-backend/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RegisterSubscriptionRoutes(router *gin.Engine, db *gorm.DB, paymentService *services.PaymentService) {
	subscriptionService := &services.SubscriptionService{
		DB:             db,
		PaymentService: paymentService,
	}
	subscriptionController := &controller.SubscriptionController{SubscriptionService: subscriptionService}

	api := router.Group("/api/subscriptions")
	{
		api.GET("/plans", subscriptionController.ListPlans)
		api.GET("/me", middleware.RoleMiddleware(), subscriptionController.GetMySubscription)
		api.POST("/checkout", middleware.RoleMiddleware(), subscriptionController.CreatePremiumCheckout)
		api.POST("/payment-notification", subscriptionController.HandlePaymentNotification)
	}
}
