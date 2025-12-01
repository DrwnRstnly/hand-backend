package config

import (
	"fmt"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func NewPostgresql(migrations ...any) *gorm.DB {
	gormLogger := logger.Default
	if Env.ENV != "production" {
		gormLogger = gormLogger.LogMode(logger.Warn) // Logs everything in non-production environments
	} else {
		gormLogger = gormLogger.LogMode(logger.Silent) // Disable logging in production
	}

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN: fmt.Sprintf(
			"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
			Env.PostgresHost,
			Env.PostgresUser,
			Env.PostgresPassword,
			Env.PostgresDbName,
			Env.PostgresPort,
		),
		PreferSimpleProtocol: true, // disables implicit prepared statement usage
	}), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`).Error; err != nil {
		log.Fatalf("failed to enable uuid-ossp extension: %v", err)
	}

	// Create enums before migrating the tables
	createEnums(db)

	if err := migratePostgresqlTables(db, migrations...); err != nil {
		log.Fatalln(err)
	}

	return db
}

func createEnums(db *gorm.DB) {
	enums := map[string]string{
		"role_enum":                        "CREATE TYPE role_enum AS ENUM ('admin', 'patient', 'therapist');",
		"appointment_schedule_status_enum": "CREATE TYPE appointment_schedule_status_enum AS ENUM ('success', 'canceled');",
		"consultation_enum":                "CREATE TYPE consultation_enum AS ENUM ('online', 'offline', 'hybrid');",
		"media_enum":                       "CREATE TYPE media_enum AS ENUM ('article', 'video');",
		"midtrans_status":                  "CREATE TYPE midtrans_status AS ENUM ('challenge', 'pending', 'failure', 'success');",
		"room_enum":                        "CREATE TYPE room_enum AS ENUM ('consultation', 'anonymous');",
		"subscription_plan_enum":           "CREATE TYPE subscription_plan_enum AS ENUM ('free', 'premium');",
		"subscription_status_enum":         "CREATE TYPE subscription_status_enum AS ENUM ('pending', 'active', 'expired', 'canceled');",
		// Add more enums as needed
	}

	for enumName, createQuery := range enums {
		if !checkEnumExists(db, enumName) {
			if err := db.Exec(createQuery).Error; err != nil {
				log.Printf("Error creating enum %s: %v\n", enumName, err)
			} else {
				log.Printf("Created enum type '%s'\n", enumName)
			}
		} else {
			log.Printf("Enum %s already exists\n", enumName)
		}
	}
}

func checkEnumExists(db *gorm.DB, enumName string) bool {
	var exists bool
	query := `SELECT EXISTS (
        SELECT 1 
        FROM pg_type 
        WHERE typname = ?
    )`
	db.Raw(query, enumName).Scan(&exists)
	return exists
}

func migratePostgresqlTables(db *gorm.DB, migrations ...any) error {

	if err := db.AutoMigrate(
		migrations..., // BREAKING: entities should be passed from cmd/api/main.go due to circular dependency issue
	); err != nil {
		return err
	}

	return nil
}
