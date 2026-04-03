package scraper

import (
	"context"
	"errors"
	"fmt"
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

	log.Println("ChatGPT: Navigate")

	err := chromedp.Run(ctx,
		chromedp.Navigate("https://chat.openai.com/"),
	)
	if err != nil {
		return result, err
	}

	time.Sleep(6 * time.Second)

	// ---------------- WAIT FOR INPUT ----------------
	log.Println("ChatGPT: Waiting for input")

	err = chromedp.Run(ctx,
		chromedp.WaitVisible(`#prompt-textarea`, chromedp.ByID),
	)
	if err != nil {
		return result, errors.New("input not found")
	}

	time.Sleep(500 * time.Millisecond)

	// ---------------- ENABLE WEB SEARCH ----------------
	log.Println("ChatGPT: Enabling web search")

	// Open plus menu
	err = chromedp.Run(ctx,
		chromedp.Click(`button[data-testid="composer-plus-btn"]`, chromedp.ByQuery),
	)
	if err != nil {
		log.Println("ChatGPT: Plus button not found:", err)
	} else {
		time.Sleep(1 * time.Second)

		enableWebSearchJS := `(() => {
			let items = document.querySelectorAll('[role="menuitemradio"]');

			for (let item of items) {
				if (item.innerText && item.innerText.includes("Web search")) {
					let btn = item.querySelector('button, div');
					if (btn) btn.click();
					else item.click();

					return item.getAttribute("aria-checked");
				}
			}
			return "not_found";
		})()`

		var status string
		err = chromedp.Run(ctx, chromedp.Evaluate(enableWebSearchJS, &status))
		log.Println("ChatGPT: Web search click status:", status)

		// Verify enabled
		verifyJS := `(() => {
			let items = document.querySelectorAll('[role="menuitemradio"]');
			for (let item of items) {
				if (item.innerText.includes("Web search")) {
					return item.getAttribute("aria-checked");
				}
			}
			return "false";
		})()`

		for i := 0; i < 5; i++ {
			var checked string
			chromedp.Run(ctx, chromedp.Evaluate(verifyJS, &checked))

			if checked == "true" {
				log.Println("ChatGPT: Web search confirmed enabled")
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	time.Sleep(1 * time.Second)

	// ---------------- TYPE QUERY ----------------
	log.Println("ChatGPT: Typing query")

	typeQueryJS := fmt.Sprintf(`(() => {
		const editor = document.getElementById('prompt-textarea');
		if (!editor) return false;

		const p = editor.querySelector('p');
		if (p) {
			p.textContent = %q;
		} else {
			editor.textContent = %q;
		}

		editor.dispatchEvent(new Event('input', { bubbles: true }));
		return true;
	})()`, query, query)

	err = chromedp.Run(ctx,
		chromedp.Evaluate(typeQueryJS, nil),
	)
	if err != nil {
		return result, errors.New("failed to type query")
	}

	time.Sleep(500 * time.Millisecond)

	// ---------------- SUBMIT ----------------
	log.Println("ChatGPT: Submitting query")

	err = chromedp.Run(ctx,
		chromedp.Click(`button#composer-submit-button`, chromedp.ByQuery),
	)

	if err != nil {
		err = chromedp.Run(ctx,
			chromedp.Click(`button[aria-label="Send message"]`, chromedp.ByQuery),
		)
		if err != nil {
			err = chromedp.Run(ctx,
				chromedp.KeyEvent("\n"),
			)
			if err != nil {
				return result, errors.New("failed to submit query")
			}
		}
	}

	log.Println("ChatGPT: Query submitted")

	// ---------------- WAIT FOR RESPONSE ----------------
	log.Println("ChatGPT: Waiting for response")

	time.Sleep(3 * time.Second)

	stableCount := 0
	var lastLen int
	var lastContent string

	for i := 0; i < 40; i++ {
		var current string

		getTextJS := fmt.Sprintf(`(() => {
			let main = document.querySelector('main');
			if (!main) return "";

			let text = main.innerText || "";
			let query = %q;

			let idx = text.lastIndexOf(query);
			if (idx > -1) {
				return text.substring(idx + query.length).trim();
			}
			return "";
		})()`, query)

		chromedp.Run(ctx, chromedp.Evaluate(getTextJS, &current))

		lastContent = current
		currLen := len(current)

		if currLen > 20 && currLen == lastLen {
			stableCount++
			if stableCount >= 3 {
				content = current
				break
			}
		} else {
			if currLen != lastLen {
				stableCount = 0
			}
			lastLen = currLen
		}

		time.Sleep(2 * time.Second)
	}

	if content == "" {
		content = lastContent
	}

	log.Println("ChatGPT: final content length:", len(content))

	// ---------------- EXTRACT LINKS ----------------
	log.Println("ChatGPT: Extracting links")

	// Try opening sources panel
	sourcesCtx, cancelSources := context.WithTimeout(ctx, 3*time.Second)
	defer cancelSources()

	chromedp.Run(sourcesCtx,
		chromedp.Click(`button[aria-label="Sources"]`, chromedp.ByQuery),
	)

	time.Sleep(1 * time.Second)

	linksJS := `(() => {
		let links = new Set();

		// Priority: citation links
		document.querySelectorAll('a[href*="utm_source=chatgpt.com"]').forEach(a => {
			if (a.href) links.add(a.href);
		});

		// Fallback
		if (links.size === 0) {
			document.querySelectorAll('main a').forEach(a => {
				if (a.href && a.href.startsWith("http") && !a.href.includes("openai")) {
					links.add(a.href);
				}
			});
		}

		return Array.from(links);
	})()`

	err = chromedp.Run(ctx, chromedp.Evaluate(linksJS, &links))
	if err != nil {
		log.Println("ChatGPT: link extraction error:", err)
	}

	log.Printf("ChatGPT: extracted %d raw links", len(links))

	result.InternalLinks = parser.CleanLinks(links)

	if content == "" {
		return result, errors.New("no content extracted")
	}

	return result, nil
}
