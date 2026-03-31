package scraper

import (
	"context"

	"github.com/yash-at-DX/ai-scraper/internal/models"
)

type Scraper interface {
	Scrape(ctx context.Context, query string) (models.Result, error)
	Name() string
}
