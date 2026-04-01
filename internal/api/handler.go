package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yash-at-DX/ai-scraper/internal/service"
	"github.com/yash-at-DX/ai-scraper/internal/storage"
)

type Request struct {
	Queries []string `json:"queries"`
}

func RegisterRoutes(r *gin.Engine) {
	r.POST("/scrape", HandleScrape)
	r.GET("/status/:id", HandleStatus)
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

	jobID := service.CreateJob()
	go service.ProcessQueriesWithJob(req.Queries, jobID)

	c.JSON(http.StatusOK, gin.H{
		"message": "scraping started",
		"job_id":  jobID,
	})
}

func HandleStatus(c *gin.Context) {
	jobID := strings.TrimSpace(c.Param("id"))

	var status string
	var errMsg string

	query := `SELECT status, error FROM ai_scrape_jobs WHERE id = ? `

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := storage.DB.QueryRowContext(ctx, query, jobID).Scan(&status, &errMsg)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "job not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"job_id": jobID,
		"status": status,
		"error":  errMsg,
	})
}
