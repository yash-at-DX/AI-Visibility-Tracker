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

type GeminiScraper struct {
	Browser *browser.Browser
}

func NewGeminiScraper(b *browser.Browser) *GeminiScraper {
	return &GeminiScraper{Browser: b}
}

func (g *GeminiScraper) Name() string {
	return "gemini"
}

func (g *GeminiScraper) Scrape(ctx context.Context, query string) (models.Result, error) {
	ctx, cancel := g.Browser.NewContext()
	defer cancel()

	result := models.Result{
		Query:  query,
		Source: g.Name(),
	}

	var content string
	var links []string

	// ---------------- STEP 1: NAVIGATE ----------------
	log.Println("Gemini: Navigate")

	err := chromedp.Run(ctx,
		chromedp.Navigate("https://gemini.google.com/"),
	)
	if err != nil {
		return result, err
	}

	time.Sleep(6 * time.Second)

	// ---------------- STEP 2: TYPE QUERY ----------------
	log.Println("Typing query")

	// Step 1: ensure page is ready
	time.Sleep(2 * time.Second)

	// Step 2: click editor via JS (more reliable than chromedp.Click)
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`
			(() => {
				let el = document.querySelector('.ql-editor');
				if (el) {
					el.click();
					return true;
				}
				return false;
			})()
		`, nil),
	)

	if err != nil {
		log.Println("JS click failed, trying mouse click fallback")

		// fallback: click center screen
		chromedp.Run(ctx,
			chromedp.MouseClickXY(600, 500),
		)
	}

	time.Sleep(1 * time.Second)

	// Step 3: insert text (THIS is key)
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`
			document.execCommand('insertText', false, "`+query+`");
		`, nil),
	)

	if err != nil {
		return result, errors.New("failed to insert text")
	}

	// Step 4: press enter
	time.Sleep(500 * time.Millisecond)

	err = chromedp.Run(ctx,
		chromedp.KeyEvent("\n"),
	)

	if err != nil {
		return result, err
	}

	log.Println("Query submitted")

	// ---------------- STEP 3: WAIT FOR FULL RESPONSE ----------------
	log.Println("Waiting for response")

	stableCount := 0
	var lastLen int

	for i := 0; i < 15; i++ {
		var current string

		err := chromedp.Run(ctx,
			chromedp.Evaluate(`(() => {
				let els = document.querySelectorAll('.markdown-main-panel');
				if (!els.length) return "";
				return els[els.length - 1].innerText;
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

		time.Sleep(2 * time.Second)
	}

	log.Println("Final content length:", len(content))

	// ---------------- STEP 4: CLICK SOURCE BUTTON ----------------
	log.Println("Opening sources")

	err = chromedp.Run(ctx,
		chromedp.Click(`button[aria-label*="source"]`, chromedp.ByQuery),
	)

	if err != nil {
		log.Println("⚠️ Could not click source button:", err)
	}

	time.Sleep(2 * time.Second)

	// ---------------- STEP 5: EXTRACT LINKS ----------------
	log.Println("Extracting links")

	err = chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			let panel = document.querySelector('context-sidebar');
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
