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

// func RunScrapers(ctx context.Context, b *browser.Browser, query string, projectID string, missingSources []string) []models.Result {

// 	// build only the scrapers that are missing
// 	sourceMap := map[string]scraper.Scraper{
// 		"chatgpt":    scraper.NewChatGPTScraper(b),
// 		"gemini":     scraper.NewGeminiScraper(b),
// 		"google_ai":  scraper.NewGoogleAIScraper(b),
// 		"perplexity": scraper.NewPerplexityScraper(b),
// 	}

// 	var scrapers []scraper.Scraper
// 	for _, s := range missingSources {
// 		if sc, ok := sourceMap[s]; ok {
// 			scrapers = append(scrapers, sc)
// 		}
// 	}

// 	var wg sync.WaitGroup
// 	resultCh := make(chan models.Result, len(scrapers))

// 	for _, s := range scrapers {
// 		wg.Add(1)
// 		go func(sc scraper.Scraper) {
// 			defer wg.Done()
// 			res, err := sc.Scrape(ctx, query)
// 			if err != nil {
// 				log.Printf("[%s] scrape failed: %v\n", sc.Name(), err)
// 				return
// 			}
// 			res.ProjectID = projectID
// 			resultCh <- res
// 		}(s)
// 	}

// 	wg.Wait()
// 	close(resultCh)

// 	var results []models.Result
// 	for r := range resultCh {
// 		results = append(results, r)
// 	}
// 	return results
// }

func RunAllScrapers(queries []models.VisibilityQuery) {
	for i, q := range queries {
		log.Printf("[%d/%d] Processing: %s\n", i+1, len(queries), q.Query)
		ProcessQuery(q)
	}
}

func ProcessQuery(q models.VisibilityQuery) {
	type scraperJob struct {
		name    string
		factory func(*browser.Browser) scraper.Scraper
	}

	jobs := []scraperJob{
		{
			name:    "chatgpt",
			factory: func(b *browser.Browser) scraper.Scraper { return scraper.NewChatGPTScraper(b) },
		},
		{
			name:    "gemini",
			factory: func(b *browser.Browser) scraper.Scraper { return scraper.NewGeminiScraper(b) },
		},
		{
			name:    "perplexity",
			factory: func(b *browser.Browser) scraper.Scraper { return scraper.NewPerplexityScraper(b) },
		},
		{
			name:    "google_ai",
			factory: func(b *browser.Browser) scraper.Scraper { return scraper.NewGoogleAIScraper(b) },
		},
	}

	var wg sync.WaitGroup

	for _, job := range jobs {
		already, err := storage.IsAlreadyScraped(q.ProjectID, q.Query, job.name)
		if err != nil {
			log.Printf("[%s] DB check failed: %v\n", job.name, err)
		}
		if already {
			log.Printf("[%s] skipping: %s\n", job.name, q.Query)
			continue
		}

		wg.Add(1)
		go func(j scraperJob) {
			defer wg.Done()

			b := browser.NewBrowser(true)
			defer func() {
				b.Close()
				log.Printf("[%s] browser closed\n", j.name)
			}()

			sc := j.factory(b)

			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
			defer cancel()

			res, err := sc.Scrape(ctx, q.Query)
			if err != nil {
				log.Printf("[%s] scrape failed: %v\n", j.name, err)
				return
			}

			// consistent rule: never insert if no links found
			if len(res.InternalLinks) == 0 {
				log.Printf("[%s] no links found, skipping insert\n", j.name)
				return
			}

			res.ProjectID = q.ProjectID
			res.Category = q.Category
			res.SearchVolume = q.SearchVolume
			res.Intent = q.Intent

			if err := storage.InsertResults([]models.Result{res}); err != nil {
				log.Printf("[%s] insert failed: %v\n", j.name, err)
			} else {
				log.Printf("[%s] done - %d links saved\n", j.name, len(res.InternalLinks))
			}
		}(job)
	}

	wg.Wait()
	log.Printf("Query complete: %s\n", q.Query)
}
