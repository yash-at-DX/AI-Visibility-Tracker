package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/yash-at-DX/ai-scraper/internal/api"
	"github.com/yash-at-DX/ai-scraper/internal/storage"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found")
	}
	storage.InitDB()

	r := gin.Default()

	api.RegisterRoutes(r)

	log.Println("Server running on : 8001")
	r.Run(":" + os.Getenv("PORT"))
}
