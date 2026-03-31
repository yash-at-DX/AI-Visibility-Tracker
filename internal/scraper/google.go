package scraper

import (
	"context"
	"errors"
	"log"
	"net/url"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/yash-at-DX/ai-scraper/internal/browser"
	"github.com/yash-at-DX/ai-scraper/internal/models"
	"github.com/yash-at-DX/ai-scraper/internal/parser"
)

type GoogleAIScraper struct {
	Browser *browser.Browser
}

func NewGoogleAIScraper(b *browser.Browser) *GoogleAIScraper {
	return &GoogleAIScraper{Browser: b}
}

func (g *GoogleAIScraper) Name() string {
	return "google_ai"
}

func (g *GoogleAIScraper) Scrape(ctx context.Context, query string) (models.Result, error) {
	ctx, cancel := g.Browser.NewContext()
	defer cancel()

	result := models.Result{
		Query:  query,
		Source: g.Name(),
	}

	var content string
	var links []string

	// ---------------- STEP 1: NAVIGATE ----------------
	searchURL := "https://www.google.com/search?q=" + url.QueryEscape(query) + "&udm=50"

	log.Println("Google AI: Navigate")

	err := chromedp.Run(ctx,
		chromedp.Navigate(searchURL),
	)
	if err != nil {
		return result, err
	}

	log.Println("Waiting for AI block")

	err = chromedp.Run(ctx,
		chromedp.WaitVisible(`body`, chromedp.ByQuery),
	)
	if err != nil {
		return result, err
	}

	time.Sleep(3 * time.Second)

	// ---------------- STEP 2: WAIT FOR CONTENT (STABLE) ----------------
	log.Println("Waiting for content to stabilize")

	stableCount := 0
	var lastLen int

	for i := 0; i < 15; i++ {
		var current string

		err := chromedp.Run(ctx,
			chromedp.Evaluate(`(() => {
				let el = document.querySelector('.pWvJNd');
				return el ? el.innerText : "";
			})()`, &current),
		)

		if err != nil {
			continue
		}

		currLen := len(current)
		log.Println("Current length:", currLen)

		if currLen > 200 && currLen == lastLen {
			stableCount++
			if stableCount >= 3 {
				content = current
				break
			}
		} else {
			stableCount = 0
			lastLen = currLen
		}

		time.Sleep(2 * time.Second)
	}

	log.Println("Final content length:", len(content))

	// ---------------- STEP 3: CLICK SOURCES ----------------
	log.Println("Opening sources tab (JS click)")

	_ = chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			let btn = Array.from(document.querySelectorAll('button, span'))
				.find(el => el.innerText && el.innerText.toLowerCase().includes('sources'));

			if (btn) {
				btn.click();
				return true;
			}
			return false;
		})()`, nil),
	)

	time.Sleep(2 * time.Second)

	// ---------------- STEP 4: WAIT FOR SOURCES ----------------
	log.Println("Waiting for sources")

	_ = chromedp.Run(ctx,
		chromedp.WaitVisible(`a.NDNGvf`, chromedp.ByQuery),
	)

	time.Sleep(1 * time.Second)

	// ---------------- STEP 5: EXTRACT SOURCES ----------------
	log.Println("Extracting sources")

	err = chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			let anchors = document.querySelectorAll('a.NDNGvf');
			let links = [];

			anchors.forEach(a => {
				if (a.href && a.href.startsWith("http")) {
					links.push(a.href);
				}
			});

			return links;
		})()`, &links),
	)

	if err != nil {
		log.Println("Source extraction error:", err)
	}

	// ---------------- FALLBACK ----------------
	if len(links) == 0 {
		log.Println("Fallback: extracting all external links")

		_ = chromedp.Run(ctx,
			chromedp.Evaluate(`(() => {
				let anchors = document.querySelectorAll('a[href]');
				let links = new Set();

				anchors.forEach(a => {
					if (a.href.startsWith("http") &&
						!a.href.includes("google.com") &&
						!a.href.includes("accounts.google")) {
						links.add(a.href);
					}
				});

				return Array.from(links);
			})()`, &links),
		)
	}

	// ---------------- FINAL ----------------
	result.Content = content
	result.InternalLinks = parser.CleanLinks(links)

	if result.Content == "" {
		return result, errors.New("no content extracted")
	}

	return result, nil
}
