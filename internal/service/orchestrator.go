package service

import (
	"context"
	"log"
	"strings"

	"github.com/yash-at-DX/ai-scraper/internal/models"
	"github.com/yash-at-DX/ai-scraper/internal/storage"
)

// RunAllScrapers runs the cron flow for every query.
// platforms: nil/empty = all platforms; otherwise only the named ones.
func RunAllScrapers(queries []models.VisibilityQuery, platforms []string) {
	if len(platforms) > 0 {
		log.Printf("Platform filter active: %s\n", strings.Join(platforms, ", "))
	}
	for i, q := range queries {
		log.Printf("[%d/%d] Processing: %s\n", i+1, len(queries), q.Query)
		ProcessQuery(q, platforms)
	}
}

// ProcessQuery is the cron flow for a single DB-sourced query.
// platforms: nil/empty = all platforms (passed through from RunAllScrapers).
func ProcessQuery(q models.VisibilityQuery, platforms []string) {
	// Resolve which platforms to consider — either the global filter or all.
	toConsider, err := ResolvePlatforms(platforms)
	if err != nil {
		log.Printf("invalid platform filter: %v\n", err)
		return
	}

	// Per-source dedupe: skip sources already scraped today.
	var pending []string
	for _, name := range toConsider {
		already, err := storage.IsAlreadyScraped(q.ProjectID, q.Query, name)
		if err != nil {
			log.Printf("[%s] DB check failed: %v\n", name, err)
		}
		if already {
			log.Printf("[%s] skipping (already scraped today): %s\n", name, q.Query)
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
