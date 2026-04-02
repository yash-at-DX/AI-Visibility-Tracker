package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/yash-at-DX/ai-scraper/internal/browser"
	"github.com/yash-at-DX/ai-scraper/internal/models"
	"github.com/yash-at-DX/ai-scraper/internal/scraper"
)

type WebhookPayload struct {
	JobID     string          `json:"job_id"`
	Status    string          `json:"status"`
	Results   []models.Result `json:"results,omitempty"`
	Error     string          `json:"error,omitempty"`
	Timestamp int64           `json:"timestamp"`
}

func processInternal(queries []string) ([]models.Result, error) {

	b := browser.NewBrowser(true)
	defer b.Close()

	scrapers := []scraper.Scraper{
		scraper.NewChatGPTScraper(b),
		scraper.NewGeminiScraper(b),
		scraper.NewGoogleAIScraper(b),
		scraper.NewPerplexityScraper(b),
	}

	var finalResults []models.Result
	var mu sync.Mutex
	var wg sync.WaitGroup

	workerLimit := 3
	sem := make(chan struct{}, workerLimit)

	for _, q := range queries {
		wg.Add(1)
		sem <- struct{}{}

		go func(query string) {
			defer wg.Done()
			defer func() { <-sem }()

			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
			defer cancel()

			results := RunScrapers(ctx, scrapers, query)

			mu.Lock()
			finalResults = append(finalResults, results...)
			mu.Unlock()
		}(q)

	}
	wg.Wait()

	return finalResults, nil
}

func RunScrapers(ctx context.Context, scrapers []scraper.Scraper, query string) []models.Result {
	var wg sync.WaitGroup
	resultCh := make(chan models.Result, len(scrapers))

	for _, s := range scrapers {
		wg.Add(1)

		go func(sc scraper.Scraper) {
			defer wg.Done()

			res, err := sc.Scrape(ctx, query)
			if err != nil {
				log.Printf("%s failed: %v\n", sc.Name(), err)
				return
			}

			resultCh <- res
		}(s)

	}
	wg.Wait()
	close(resultCh)

	var results []models.Result
	for r := range resultCh {
		results = append(results, r)
	}

	return results
}

func ProcessQueriesWithWebhook(queries []string, jobID string, webhookURL string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Job %s panicked: %v", jobID, r)
			sendWebhook(webhookURL, WebhookPayload{
				JobID:     jobID,
				Status:    "failed",
				Error:     fmt.Sprintf("panic occured: %v", r),
				Timestamp: time.Now().Unix(),
			})
		}
	}()

	log.Printf("Starting job %s with %d queries ", jobID, len(queries))
	results, err := processInternal(queries)

	if err != nil {
		log.Printf("Job %s failed: %v", jobID, err)
		sendWebhook(webhookURL, WebhookPayload{
			JobID:     jobID,
			Status:    "failed",
			Error:     err.Error(),
			Timestamp: time.Now().Unix(),
		})
		return
	}

	log.Printf("Job %s completed successfully with %d results", jobID, len(results))
	sendWebhook(webhookURL, WebhookPayload{
		JobID:     jobID,
		Status:    "completed",
		Results:   results,
		Timestamp: time.Now().Unix(),
	})
}

func sendWebhook(webhookURL string, payload WebhookPayload) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal webhook payload: %v", err)
		return
	}

	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		client := &http.Client{
			Timeout: 30 * time.Second,
		}

		resp, err := client.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))

		if err != nil {
			log.Printf("Webhook attempt %d/%d failed: %v", attempt+1, maxRetries, err)
			if attempt < maxRetries-1 {
				time.Sleep(time.Duration(2<<uint(attempt)) * time.Second)
				continue
			}
			log.Printf("All webhook attempts failed for job %s", payload.JobID)
			return
		}

		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			log.Printf("Webhook sent successfully for job %s (status: %d)", payload.JobID, resp.StatusCode)
			return
		}

		log.Printf("Webhook returned non-2xx status: %d", resp.StatusCode)
		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(2<<uint(attempt)) * time.Second)
		}
	}

	log.Printf("Failed to send webhook after %d attempts for job %s", maxRetries, payload.JobID)
}
