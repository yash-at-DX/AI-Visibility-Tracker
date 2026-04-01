package scraper

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/yash-at-DX/ai-scraper/internal/browser"
	"github.com/yash-at-DX/ai-scraper/internal/models"
	"github.com/yash-at-DX/ai-scraper/internal/parser"
)

type PerplexityScraper struct {
	Browser *browser.Browser
}

func NewPerplexityScraper(b *browser.Browser) *PerplexityScraper {
	return &PerplexityScraper{Browser: b}
}

func (p *PerplexityScraper) Name() string {
	return "perplexity"
}

func (p *PerplexityScraper) Scrape(ctx context.Context, query string) (models.Result, error) {
	ctx, cancel := p.Browser.NewContext()
	defer cancel()

	result := models.Result{
		Query:  query,
		Source: p.Name(),
	}

	var content string
	var links []string

	// ---------------- STEP 1: NAVIGATE ----------------
	log.Println("Step 1: Navigate")

	err := chromedp.Run(ctx,
		chromedp.Navigate("https://www.perplexity.ai/"),
	)
	if err != nil {
		return result, err
	}

	time.Sleep(5 * time.Second)

	// ---------------- STEP 2: VERIFY PAGE ----------------
	var title string
	err = chromedp.Run(ctx,
		chromedp.Title(&title),
	)
	if err != nil {
		return result, err
	}
	log.Println("Page title:", title)

	// ---------------- STEP 3: TYPE QUERY ----------------
	log.Println("Step 3: Typing query")

	err = chromedp.Run(ctx,
		chromedp.WaitVisible(`#ask-input`, chromedp.ByQuery),
		chromedp.Click(`#ask-input`, chromedp.ByQuery),
		chromedp.SendKeys(`#ask-input`, query),
		chromedp.SendKeys(`#ask-input`, "\n"),
	)
	if err != nil {
		return result, errors.New("failed to type query")
	}

	log.Println("Query submitted")

	// ---------------- STEP 4: WAIT FOR RESPONSE START ----------------
	log.Println("Step 4: Waiting for response start")

	started := false

	for i := 0; i < 10; i++ {
		var txt string

		chromedp.Run(ctx,
			chromedp.Evaluate(`document.body.innerText`, &txt),
		)

		if len(txt) > 300 {
			started = true
			break
		}

		time.Sleep(2 * time.Second)
	}

	if !started {
		return result, errors.New("response did not start")
	}

	// ---------------- STEP 5: WAIT FOR FULL RESPONSE ----------------
	log.Println("Step 5: Waiting for full response (stability check)")

	stableCount := 0
	var lastLen int

	for i := 0; i < 15; i++ {
		var current string

		err := chromedp.Run(ctx,
			chromedp.Evaluate(`(() => {
				let el = document.querySelector('[id^="markdown-content"]');
				return el ? el.innerText : "";
			})()`, &current),
		)

		if err != nil {
			continue
		}

		currentLen := len(current)
		log.Println("Current length:", currentLen)

		if currentLen > 200 && currentLen == lastLen {
			stableCount++
			if stableCount >= 3 {
				content = current
				break
			}
		} else {
			stableCount = 0
			lastLen = currentLen
		}

		// early break for long responses
		if currentLen > 1500 {
			content = current
			break
		}

		time.Sleep(2 * time.Second)
	}

	log.Println("Final content length:", len(content))

	// ---------------- STEP 6: EXTRACT LINKS FROM LINKS TAB ----------------
	log.Println("Step 6: Extracting links (Links tab)")

	// Click "Links" tab
	err = chromedp.Run(ctx,
		chromedp.Click(`button[role="tab"][aria-controls*="sources"]`, chromedp.ByQuery),
	)

	if err != nil {
		log.Println("⚠️ Could not click Links tab:", err)
	}

	time.Sleep(2 * time.Second)

	// Extract links from panel
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			let panel = document.querySelector('[role="tabpanel"][id*="sources"]');
			if (!panel) return [];

			let links = new Set();

			panel.querySelectorAll("a[href]").forEach(a => {
				if (a.href && a.href.startsWith("http")) {
					links.add(a.href);
				}
			});

			return Array.from(links);
		})()`, &links),
	)

	if err != nil {
		log.Println("Link extraction error:", err)
	}

	// ---------------- FINAL ----------------
	// result.Content = content
	result.InternalLinks = parser.CleanLinks(links)

	if content == "" {
		return result, errors.New("no content extracted")
	}

	return result, nil
}
