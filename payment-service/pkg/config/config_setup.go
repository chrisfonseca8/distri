package config

import (
	"github.com/chrisfonseca08/distri/payment-service/pkg/payment_gateway"
	"gorm.io/gorm"
)

type Config struct {
	DB      *gorm.DB
	Gateway paymentgateway.Gateway
	// GatewayName is stored in payment_attempts.gateway ("sim" or "razorpay").
	GatewayName string
	Worker      WorkerConfig
}

// Setup loads the worker settings and opens the database.
func Setup() (*Config, error) {
	workerConfig, err := LoadWorkerConfig()
	if err != nil {
		return nil, err
	}

	db, err := OpenDB()
	if err != nil {
		return nil, err
	}

	gateway, gatewayName, err := paymentgateway.NewFromEnv()
	if err != nil {
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}

	return &Config{DB: db, Gateway: gateway, GatewayName: gatewayName, Worker: workerConfig}, nil
}
