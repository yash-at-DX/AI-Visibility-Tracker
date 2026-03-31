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

type ChatGPTScraper struct {
	Browser *browser.Browser
}

func NewChatGPTScraper(b *browser.Browser) *ChatGPTScraper {
	return &ChatGPTScraper{Browser: b}
}

func (c *ChatGPTScraper) Name() string {
	return "chatgpt"
}

func (c *ChatGPTScraper) Scrape(ctx context.Context, query string) (models.Result, error) {
	ctx, cancel := c.Browser.NewContext()
	defer cancel()

	result := models.Result{
		Query:  query,
		Source: c.Name(),
	}

	var content string
	var links []string

	// ---------------- STEP 1: NAVIGATE ----------------
	log.Println("ChatGPT: Navigate")

	err := chromedp.Run(ctx,
		chromedp.Navigate("https://chat.openai.com/"),
	)
	if err != nil {
		return result, err
	}

	time.Sleep(5 * time.Second)

	// ---------------- STEP 2: TYPE QUERY ----------------
	log.Println("Typing query")

	err = chromedp.Run(ctx,
		chromedp.WaitVisible(`p[data-placeholder="Ask anything"]`, chromedp.ByQuery),
		chromedp.Click(`p[data-placeholder="Ask anything"]`, chromedp.ByQuery),
	)
	if err != nil {
		return result, errors.New("input not found")
	}

	time.Sleep(1 * time.Second)

	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.execCommand('insertText', false, "`+query+`")`, nil),
	)
	if err != nil {
		return result, errors.New("failed to type query")
	}

	time.Sleep(500 * time.Millisecond)

	err = chromedp.Run(ctx,
		chromedp.KeyEvent("\n"),
	)
	if err != nil {
		return result, err
	}

	log.Println("Query submitted")

	// ---------------- STEP 3: WAIT FOR RESPONSE ----------------
	log.Println("Waiting for response")

	stableCount := 0
	var lastLen int
	var lastContent string

	for i := 0; i < 15; i++ {
		var current string

		err := chromedp.Run(ctx,
			chromedp.Evaluate(`(() => {
				let els = document.querySelectorAll('.markdown.prose');
				if (!els.length) return "";
				return els[els.length - 1].innerText;
			})()`, &current),
		)

		if err != nil {
			continue
		}

		lastContent = current // 🔥 store latest always

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

	// 🔥 fallback fix (IMPORTANT)
	if content == "" {
		log.Println("Fallback: using last captured content")
		content = lastContent
	}

	log.Println("Final content length:", len(content))

	// ---------------- STEP 4: CHECK SOURCES BUTTON ----------------
	log.Println("Checking for sources")

	var hasSources bool

	_ = chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			return !!document.querySelector('button[aria-label="Sources"]');
		})()`, &hasSources),
	)

	// ---------------- STEP 5: EXTRACT LINKS ----------------
	if hasSources {
		log.Println("Opening sources")

		_ = chromedp.Run(ctx,
			chromedp.Evaluate(`(() => {
				let btn = document.querySelector('button[aria-label="Sources"]');
				if (btn) {
					btn.click();
					return true;
				}
				return false;
			})()`, nil),
		)

		time.Sleep(2 * time.Second)

		log.Println("Extracting sources")

		_ = chromedp.Run(ctx,
			chromedp.Evaluate(`(() => {
				let anchors = document.querySelectorAll('ul.flex.flex-col li a[href]');
				let links = [];

				anchors.forEach(a => {
					if (a.href && a.href.startsWith("http")) {
						links.push(a.href);
					}
				});

				return links;
			})()`, &links),
		)
	}

	// ---------------- FALLBACK LINKS ----------------
	if len(links) == 0 {
		log.Println("Fallback: extracting links from content")

		_ = chromedp.Run(ctx,
			chromedp.Evaluate(`(() => {
				let anchors = document.querySelectorAll('.markdown.prose a[href]');
				let links = [];

				anchors.forEach(a => {
					if (a.href && a.href.startsWith("http")) {
						links.push(a.href);
					}
				});

				return links;
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
