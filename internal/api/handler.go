package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yash-at-DX/ai-scraper/internal/service"
)

type Request struct {
	Queries    []string `json:"queries" binding:"required"`
	WebhookURL string   `json:"webhook_url" binding:"required,url"`
}

func RegisterRoutes(r *gin.Engine) {
	r.POST("/scrape", HandleScrape)
	r.GET("/health", HandleHealth)
}

func HandleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
	})
}

func HandleScrape(c *gin.Context) {
	var req Request

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid Request",
		})
		return
	}

	if len(req.Queries) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "queries required",
		})
		return
	}

	jobID := service.GenerateJobID()

	go service.ProcessQueriesWithWebhook(req.Queries, jobID, req.WebhookURL)

	c.JSON(http.StatusAccepted, gin.H{
		"message": "scraping started",
		"job_id":  jobID,
		"status":  "processing",
	})
}
