package database

import (
	"log"
	"context"
	"time"

	"github.com/chrisfonseca08/distri/order-service/database/config"
)

func TestDbHealth(){

	db,err := config.OpenTestDB()
	if err!=nil{
		log.Fatalln("database connection failed",err)
	}

	sqlDB,err := db.DB()
	if err != nil {
		log.Fatal(err)
	}
	defer sqlDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		log.Fatal("Database health check failed: ", err)
	}

	log.Println("database connetion found")

}