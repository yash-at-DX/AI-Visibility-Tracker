package service

import (
	"context"
	"log"

	"github.com/yash-at-DX/ai-scraper/internal/models"
	"github.com/yash-at-DX/ai-scraper/internal/storage"
)

func RunAllScrapers(queries []models.VisibilityQuery) {
	for i, q := range queries {
		log.Printf("[%d/%d] Processing: %s\n", i+1, len(queries), q.Query)
		ProcessQuery(q)
	}
}

// ProcessQuery is the cron flow for a single DB-sourced query. Behavior is
// unchanged from the original implementation:
//   - per-source dedupe via IsAlreadyScraped (skip sources already done today)
//   - scrape the remaining sources
//   - never insert a result with zero links
//   - carry project/category/intent/search_volume onto the result
//
// The actual scraping is delegated to RunQuery so the cron and on-demand flows
// share one implementation.
func ProcessQuery(q models.VisibilityQuery) {
	// Decide which sources still need scraping today.
	var pending []string
	for _, name := range AllPlatforms() {
		already, err := storage.IsAlreadyScraped(q.ProjectID, q.Query, name)
		if err != nil {
			log.Printf("[%s] DB check failed: %v\n", name, err)
		}
		if already {
			log.Printf("[%s] skipping: %s\n", name, q.Query)
			continue
		}
		pending = append(pending, name)
	}

	if len(pending) == 0 {
		log.Printf("Query complete (nothing pending): %s\n", q.Query)
		return
	}

	results, err := RunQuery(context.Background(), q.Query, pending)
	if err != nil {
		log.Printf("RunQuery failed for %q: %v\n", q.Query, err)
		return
	}

	for _, res := range results {
		// consistent rule: never insert if no links found
		if len(res.InternalLinks) == 0 {
			log.Printf("[%s] no links found, skipping insert\n", res.Source)
			continue
		}

		res.ProjectID = q.ProjectID
		res.Category = q.Category
		res.SearchVolume = q.SearchVolume
		res.Intent = q.Intent

		if err := storage.InsertResults([]models.Result{res}); err != nil {
			log.Printf("[%s] insert failed: %v\n", res.Source, err)
		} else {
			log.Printf("[%s] done - %d links saved\n", res.Source, len(res.InternalLinks))
		}
	}

	log.Printf("Query complete: %s\n", q.Query)
}
