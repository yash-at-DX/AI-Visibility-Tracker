package worker

import (
	"context"
	"log"

	"github.com/yash-at-DX/ai-scraper/internal/models"
	"github.com/yash-at-DX/ai-scraper/internal/scraper"
)

func StartWorkerPool(
	ctx context.Context,
	workers int,
	queries []string,
	s scraper.Scraper,
) []models.Result {
	jobs := make(chan string)
	results := make(chan models.Result)

	for i := 0; i < workers; i++ {
		go func(id int) {
			for q := range jobs {
				res, err := s.Scrape(ctx, q)
				if err != nil {
					log.Println("Error: ", err)
					continue
				}

				results <- res
			}
		}(i)
	}

	go func() {
		for _, q := range queries {
			jobs <- q
		}
		close(jobs)
	}()

	var output []models.Result
	for i := 0; i < len(queries); i++ {
		output = append(output, <-results)
	}

	return output
}
