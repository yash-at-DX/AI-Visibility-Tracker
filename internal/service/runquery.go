package service

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/yash-at-DX/ai-scraper/internal/browser"
	"github.com/yash-at-DX/ai-scraper/internal/models"
	"github.com/yash-at-DX/ai-scraper/internal/scraper"
)

// scraperFactories is the single source of truth for which platforms exist and
// how to construct each scraper. Both the cron flow (ProcessQuery) and the
// on-demand flow (RunQuery) build their job lists from this map, so adding a
// new platform in one place wires it into both paths.
var scraperFactories = map[string]func(*browser.Browser) scraper.Scraper{
	"chatgpt":    func(b *browser.Browser) scraper.Scraper { return scraper.NewChatGPTScraper(b) },
	"gemini":     func(b *browser.Browser) scraper.Scraper { return scraper.NewGeminiScraper(b) },
	"perplexity": func(b *browser.Browser) scraper.Scraper { return scraper.NewPerplexityScraper(b) },
	"google_ai":  func(b *browser.Browser) scraper.Scraper { return scraper.NewGoogleAIScraper(b) },
}

// AllPlatforms returns every known platform name in a stable order.
func AllPlatforms() []string {
	names := make([]string, 0, len(scraperFactories))
	for name := range scraperFactories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// resolvePlatforms validates a requested platform list against the known
// factories. An empty/nil request expands to all platforms. Unknown names are
// rejected so a caller typo doesn't silently scrape nothing.
func ResolvePlatforms(requested []string) ([]string, error) {
	if len(requested) == 0 {
		return AllPlatforms(), nil
	}

	seen := make(map[string]bool)
	var resolved []string
	for _, name := range requested {
		if _, ok := scraperFactories[name]; !ok {
			return nil, fmt.Errorf("unknown platform %q (known: %v)", name, AllPlatforms())
		}
		if seen[name] {
			continue // tolerate duplicate platform names in the request
		}
		seen[name] = true
		resolved = append(resolved, name)
	}
	return resolved, nil
}

// scrapeTimeout mirrors the per-scraper timeout used by the cron flow.
const scrapeTimeout = 180 * time.Second

// RunQuery scrapes a single query across the requested platforms and returns
// the results. It performs NO database work (no dedupe check, no insert) and
// does NOT apply the "skip when zero links" rule — callers decide what to do
// with the results. This is the shared core used by both the cron flow and the
// on-demand flow.
//
// platforms: empty/nil means all platforms; otherwise only the named ones.
// Unknown platform names cause an error before any scraping starts.
//
// Each platform runs in its own goroutine with its own headless browser, just
// like the cron flow. A per-scraper failure is logged and that platform is
// simply absent from the returned slice; it does not fail the whole query.
func RunQuery(ctx context.Context, query string, platforms []string) ([]models.Result, error) {
	resolved, err := ResolvePlatforms(platforms)
	if err != nil {
		return nil, err
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []models.Result
	)

	for _, name := range resolved {
		factory := scraperFactories[name]

		wg.Add(1)
		go func(platform string, factory func(*browser.Browser) scraper.Scraper) {
			defer wg.Done()

			b := browser.NewBrowser(true)
			defer func() {
				b.Close()
				log.Printf("[%s] browser closed\n", platform)
			}()

			sc := factory(b)

			sctx, cancel := context.WithTimeout(ctx, scrapeTimeout)
			defer cancel()

			res, err := sc.Scrape(sctx, query)
			if err != nil {
				log.Printf("[%s] scrape failed: %v\n", platform, err)
				return
			}

			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}(name, factory)
	}

	wg.Wait()
	return results, nil
}
