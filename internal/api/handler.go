package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yash-at-DX/ai-scraper/internal/service"
)

type Request struct {
	Queries []string `json:"queries"`
}

func RegisterRoutes(r *gin.Engine) {
	r.POST("/scrape", HandleScrape)
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

	results, err := service.ProcessQueries(req.Queries)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"results": results,
	})
}
