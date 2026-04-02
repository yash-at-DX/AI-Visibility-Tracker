package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/yash-at-DX/ai-scraper/internal/api"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found")
	}
	// storage.InitDB()

	r := gin.Default()

	// r.Use(func(c *gin.Context) {
	// 	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	// 	c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	// c.Writer.Header().Set("Access-Control-Allow-Headers","Content-Type", "Authorization")

	// 	if c.Request.Method == "OPTIONS" {
	// 		c.AbortWithStatus(204)
	// 		return
	// 	}

	// 	c.Next()
	// })

	api.RegisterRoutes(r)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8001"
	}

	log.Printf("Server running on : %s", port)
	r.Run(":" + port)
}
