package main

import (
	"fmt"
	"log"
	"os"

	"github.com/Hand-TBN1/hand-backend/config"
	"github.com/Hand-TBN1/hand-backend/middleware"
	"github.com/Hand-TBN1/hand-backend/models"
	"github.com/Hand-TBN1/hand-backend/routes"
	"github.com/Hand-TBN1/hand-backend/services"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
)

func main() {
	err := godotenv.Load()
	apiEnv := os.Getenv("ENV")
	if err != nil && apiEnv == "" {
		log.Println("fail to load env", err)
	}
	config.LoadEnv()

	// Pass all models to the migration function
	db := config.NewPostgresql(
		&models.User{},
		&models.Therapist{},
		&models.CheckIn{},
		&models.ChatMessage{},
		&models.ChatRoom{},
		&models.PositiveAffirmation{},
		&models.EmergencyHistory{},
		&models.Media{},
		&models.Journal{},
		&models.Availability{},
		&models.PersonalHealthPlan{},
		&models.Appointment{},
		&models.ConsultationHistory{},
		&models.Medication{},
		&models.Prescription{},
		&models.MedicationHistoryTransaction{},
		&models.MedicationHistoryItem{},
		&models.Subscription{},
	)

	redisClient := config.NewRedis()
	config.LoadR2Config()

	if redisClient != nil {
		log.Println("Connect Redis Successful")
	}

	if db != nil {
		log.Println("Connect Successful")
	} else {
		log.Println("Failed Connect")
	}

	config.SetupMidtrans()
	paymentService := &services.PaymentService{}
	checkInService := &services.CheckInService{DB: db}

	engine := config.NewGin()
	engine.Use(middleware.CORS())

	engine.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	routes.SetupAuthRoutes(engine, db)
	routes.RegisterCheckInRoutes(engine, db)
	routes.RegisterMedicationRoutes(engine, db)
	routes.RegisterMediaRoutes(engine, db)
	routes.RegisterConsultationRoutes(engine, db)
	routes.RegisterMedicationTransactionHistoryRoutes(engine, db, paymentService)
	routes.RegisterTherapistRoutes(engine, db)
	routes.SetupPaymentRoutes(engine, db)
	routes.RegisterUserRoutes(engine, db)
	routes.RegisterAppointmentRoutes(engine, db, paymentService)
	routes.RegisterJournalRoutes(engine, db)
	routes.RegisterPrescriptionRoutes(engine, db)
	routes.RegisterChatRoutes(engine, db)
	routes.RegisterSubscriptionRoutes(engine, db, paymentService)
	routes.RegisterCloudflareRoutes(engine)

	c := cron.New()
	_, err = c.AddFunc("30 16 * * *", func() {
		users, err := checkInService.CheckUserCheckIns()
		if err != nil {
			log.Println("Error fetching users:", err)
			return
		}

		for _, user := range users {
			notificationErr := checkInService.SendReminder(user.PhoneNumber)
			fmt.Println(user.PhoneNumber)
			if notificationErr != nil {
				log.Println("Error sending reminder:", notificationErr)
			}
		}
	})
	if err != nil {
		log.Fatalf("Error scheduling the task: %v", err)
	}
	c.Start()

	go func() {
		select {}
	}()

	log.Printf("Running on port %s", config.Env.ApiPort)
	if err := engine.Run(config.Env.ApiPort); err != nil {
		log.Fatalf("Failed to start server: %v\n", err)
	}
}
