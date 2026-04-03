package scraper

import (
	"context"
	"errors"
	"fmt"
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

	var links []string

	// ---------------- STEP 1: NAVIGATE ----------------
	searchURL := "https://www.google.com/search?q=" + url.QueryEscape(query) + "&udm=50"
	log.Println("Google AI: Navigate")

	err := chromedp.Run(ctx, chromedp.Navigate(searchURL))
	if err != nil {
		return result, err
	}

	time.Sleep(3 * time.Second)

	// debug: confirm page loaded correctly
	var title string
	chromedp.Run(ctx, chromedp.Title(&title))
	log.Println("Google AI: page title:", title)

	// ---------------- STEP 2: WAIT FOR AI CONTENT ----------------
	log.Println("Google AI: Waiting for content")

	// try multiple known selectors for Google AI overview block
	contentSelectors := []string{
		`.pWvJNd`, // old
		`.IVvmDb`, // alternate
		`.wDYxhc`, // another variant
		`[data-attrid="wa:/description"]`,
		`.kno-rdesc span`,
		`.LGOjhe`,
		`.vxQmIe`,
		`[jsname="bVFM4b"]`,
	}

	var content string
	stableCount := 0
	var lastLen int

	for i := 0; i < 20; i++ {
		var current string

		// try each selector until one returns content
		for _, sel := range contentSelectors {
			checkJS := fmt.Sprintf(`(() => {
				let el = document.querySelector('%s');
				return el ? el.innerText : "";
			})()`, sel)

			chromedp.Run(ctx, chromedp.Evaluate(checkJS, &current))
			if len(current) > 100 {
				break
			}
		}

		currLen := len(current)
		log.Println("Google AI: content length:", currLen)

		if currLen > 200 && currLen == lastLen {
			stableCount++
			if stableCount >= 2 {
				content = current
				break
			}
		} else {
			stableCount = 0
			lastLen = currLen
		}

		time.Sleep(2 * time.Second)
	}

	log.Println("Google AI: final content length:", len(content))

	// ---------------- STEP 3: EXTRACT LINKS ----------------
	log.Println("Google AI: Extracting links")

	// try sources panel first
	_ = chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			let btn = Array.from(document.querySelectorAll('button, span, div[role="button"]'))
				.find(el => el.innerText && el.innerText.toLowerCase().trim() === 'sources');
			if (btn) { btn.click(); return true; }
			return false;
		})()`, nil),
	)

	time.Sleep(2 * time.Second)

	// try known source link selectors
	sourcesSelectors := []string{
		`a.NDNGvf`,
		`a.cz3goc`,
		`.UJe8Uc a[href]`,
		`.yQDlj a[href]`,
		`.guvigf a[href]`,
	}

	for _, sel := range sourcesSelectors {
		linksJS := fmt.Sprintf(`(() => {
			let anchors = document.querySelectorAll('%s');
			return Array.from(anchors)
				.map(a => a.href)
				.filter(h => h.startsWith("http"));
		})()`, sel)

		chromedp.Run(ctx, chromedp.Evaluate(linksJS, &links))
		if len(links) > 0 {
			log.Printf("Google AI: found %d links with selector %s\n", len(links), sel)
			break
		}
	}

	// fallback: all external links excluding google.com
	if len(links) == 0 {
		log.Println("Google AI: fallback link extraction")
		chromedp.Run(ctx,
			chromedp.Evaluate(`(() => {
				let seen = new Set();
				return Array.from(document.querySelectorAll('a[href]'))
					.map(a => a.href)
					.filter(h => {
						if (!h.startsWith("http")) return false;
						if (h.includes("google.com")) return false;
						if (h.includes("accounts.google")) return false;
						if (seen.has(h)) return false;
						seen.add(h);
						return true;
					});
			})()`, &links),
		)
	}

	// ---------------- FINAL ----------------
	result.InternalLinks = parser.CleanLinks(links)

	if content == "" {
		// don't block saving links just because content selector missed
		if len(result.InternalLinks) > 0 {
			log.Println("Google AI: no content but has links, saving anyway")
			return result, nil
		}
		return result, errors.New("no content extracted")
	}

	return result, nil
}
