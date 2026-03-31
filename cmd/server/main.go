package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/yash-at-DX/ai-scraper/internal/api"
)

func main() {
	r := gin.Default()

	api.RegisterRoutes(r)

	log.Println("Server running on : 8001")
	r.Run(":8001")
}
