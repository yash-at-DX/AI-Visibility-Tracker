package service

import (
	"fmt"
	"log"
	"time"

	"github.com/yash-at-DX/ai-scraper/internal/storage"
)

func CreateJob() string {
	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())

	query := `INSERT INTO ai_scrape_jobs (id, status) VALUES (?, ?)`
	_, err := storage.DB.Exec(query, jobID, "running")
	if err != nil {
		log.Println("Failed to create job: ", err)
	}

	return jobID
}

func UpdateJobStatus(jobID string, status string, errMsg string) {
	query := `UPDATE ai_scrape_jobs SET status=?, error=? WHERE id=?`
	_, err := storage.DB.Exec(query, status, errMsg, jobID)
	if err != nil {
		log.Println("Failed to udpdate job status: ", err)
	}

}
