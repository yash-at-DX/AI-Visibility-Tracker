package service

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/yash-at-DX/ai-scraper/internal/browser"
	"github.com/yash-at-DX/ai-scraper/internal/models"
	"github.com/yash-at-DX/ai-scraper/internal/scraper"
	"github.com/yash-at-DX/ai-scraper/internal/storage"
)

func processInternal(queries []string) error {

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

			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()

			results := RunScrapers(ctx, scrapers, query)

			// if len(results) > 0 {
			// 	err := storage.InsertResults(results)
			// 	if err != nil {
			// 		log.Println("DB insert Failed: ", err)

			// 	}
			// }

			mu.Lock()
			finalResults = append(finalResults, results...)
			mu.Unlock()
		}(q)

	}
	wg.Wait()

	if len(finalResults) > 0 {
		err := storage.InsertResults(finalResults)
		if err != nil {
			log.Println("DB insert failed: ", err)
		}
	}

	return nil
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

func ProcessQueriesWithJob(queries []string, jobID string) {
	defer func() {
		if r := recover(); r != nil {
			UpdateJobStatus(jobID, "failed", "panic occurred")
		}
	}()

	err := processInternal(queries)

	if err != nil {
		UpdateJobStatus(jobID, "failed", err.Error())
	} else {
		UpdateJobStatus(jobID, "success", "")
	}
}
