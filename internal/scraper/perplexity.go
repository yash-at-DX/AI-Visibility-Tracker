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

// PerplexityScraper uses DOM-based extraction rather than CDP network interception.
//
// Why DOM (unlike Google AI / ChatGPT / Gemini which use CDP):
//   • Perplexity's citation URLs live in a thread-specific endpoint that's
//     hard to identify reliably, and capturing the wrong endpoint pollutes
//     the dataset with unrelated source recommendations.
//   • The Sources tab uses semantic markup — role="tab" aria-controls="sources"
//     and role="tabpanel" — which is stable across redesigns.
//   • The sources panel renders synchronously once the answer completes and
//     contains plain <a href> elements. Extraction is one line of JS.
//   • The previous DOM-based version of this scraper worked reliably; CDP
//     experimentation added complexity without benefit for this platform.
//
// What to maintain over time:
//   • If Perplexity renames the "Sources" tab, update the tab-finding JS
//     (currently uses aria-controls="sources", role="tab", and text-match
//     fallbacks).
//   • The content selector is '[id^="markdown-content"]' which has been
//     stable for >1 year; multiple fallbacks are included in case it changes.

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

	// ── STEP 1: navigate ──────────────────────────────────────────────────────
	log.Println("Perplexity: Navigate")

	err := chromedp.Run(ctx,
		chromedp.Navigate("https://www.perplexity.ai/"),
	)
	if err != nil {
		return result, err
	}

	time.Sleep(5 * time.Second)

	var title string
	chromedp.Run(ctx, chromedp.Title(&title))
	log.Println("Perplexity: page title:", title)

	// ── STEP 2: type and submit query ─────────────────────────────────────────
	log.Println("Perplexity: Typing query")

	// Try the known input selector first, then fall back to alternates.
	inputSelectors := []string{
		`#ask-input`,
		`textarea[placeholder*="Ask"]`,
		`textarea[placeholder*="ask"]`,
		`[contenteditable="true"]`,
	}

	typed := false
	for _, sel := range inputSelectors {
		err = chromedp.Run(ctx,
			chromedp.WaitVisible(sel, chromedp.ByQuery),
			chromedp.Click(sel, chromedp.ByQuery),
			chromedp.SendKeys(sel, query),
			chromedp.SendKeys(sel, "\n"),
		)
		if err == nil {
			typed = true
			break
		}
	}

	if !typed {
		return result, errors.New("failed to type query")
	}

	log.Println("Perplexity: query submitted")

	// ── STEP 3: wait for response to start ────────────────────────────────────
	log.Println("Perplexity: waiting for response start")

	started := false
	for i := 0; i < 12; i++ {
		var txt string
		chromedp.Run(ctx, chromedp.Evaluate(`document.body.innerText`, &txt))
		if len(txt) > 300 {
			started = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !started {
		return result, errors.New("response did not start")
	}

	// ── STEP 4: wait for content to stabilise ─────────────────────────────────
	log.Println("Perplexity: waiting for full response (stability check)")

	// Multiple content selectors — Perplexity has historically used these.
	// Order matters: most specific first, then progressively more permissive.
	contentJS := `(() => {
		let sels = [
			'[id^="markdown-content"]',
			'.prose',
			'[data-testid="answer"]',
			'.answer-content',
			'[class*="MarkdownContent"]',
			'[class*="Answer"]'
		];
		for (let sel of sels) {
			let els = document.querySelectorAll(sel);
			if (els.length) {
				let text = Array.from(els).map(e => e.innerText).join("\n").trim();
				if (text.length > 50) return text;
			}
		}
		return "";
	})()`

	stableCount := 0
	var lastLen int
	for i := 0; i < 20; i++ {
		var current string
		err := chromedp.Run(ctx, chromedp.Evaluate(contentJS, &current))
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		currentLen := len(current)

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

		// Early exit for long stable responses
		if currentLen > 1500 {
			content = current
			break
		}

		time.Sleep(2 * time.Second)
	}

	log.Printf("Perplexity: final content length: %d", len(content))

	// ── STEP 5: click the Sources tab ─────────────────────────────────────────
	log.Println("Perplexity: clicking Sources tab")

	// Resilient tab finder: try semantic markup first, then text-match fallbacks.
	chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		// Approach 1: tab with aria-controls pointing at sources
		let tab = document.querySelector('[role="tab"][aria-controls*="sources"]');
		if (tab) { tab.click(); return "aria_controls"; }

		// Approach 2: tab with text "Sources" or "Links"
		let tabs = Array.from(document.querySelectorAll('[role="tab"]'));
		let found = tabs.find(t => {
			let txt = (t.innerText || "").trim().toLowerCase();
			return txt === "sources" || txt === "links" ||
			       txt.startsWith("sources") ||
			       /^\d+\s+sources$/.test(txt);
		});
		if (found) { found.click(); return "text_match_tab"; }

		// Approach 3: any button with those labels
		let btns = Array.from(document.querySelectorAll('button, a'));
		let btn = btns.find(b => {
			let txt = (b.innerText || "").trim().toLowerCase();
			return txt === "sources" || txt === "links" ||
			       /^\d+\s+sources$/.test(txt);
		});
		if (btn) { btn.click(); return "text_match_button"; }

		return "not_found";
	})()`, nil))

	time.Sleep(2 * time.Second)

	// ── STEP 6: extract links from the Sources panel ──────────────────────────
	log.Println("Perplexity: extracting links")

	chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		let links = new Set();

		// Approach 1: the active sources tabpanel (semantic markup)
		let panel = document.querySelector('[role="tabpanel"][id*="sources"]') ||
		            document.querySelector('[role="tabpanel"]');
		if (panel) {
			panel.querySelectorAll('a[href]').forEach(a => {
				if (a.href && a.href.startsWith('http') &&
				    !a.href.includes('perplexity.ai')) {
					links.add(a.href);
				}
			});
		}

		// Approach 2: source-card containers (class name heuristic)
		if (links.size === 0) {
			let containers = document.querySelectorAll(
				'[class*="Source"], [class*="source"], [class*="Citation"]'
			);
			containers.forEach(c => {
				c.querySelectorAll('a[href]').forEach(a => {
					if (a.href && a.href.startsWith('http') &&
					    !a.href.includes('perplexity.ai')) {
						links.add(a.href);
					}
				});
			});
		}

		// Approach 3: hard fallback — any external link on the page that's
		// not a social share or Perplexity-owned URL.
		if (links.size === 0) {
			document.querySelectorAll('a[href]').forEach(a => {
				if (a.href && a.href.startsWith('http') &&
				    !a.href.includes('perplexity.ai') &&
				    !a.href.includes('twitter.com') &&
				    !a.href.includes('x.com') &&
				    !a.href.includes('linkedin.com/share')) {
					links.add(a.href);
				}
			});
		}

		return Array.from(links);
	})()`, &links))

	log.Printf("Perplexity: extracted %d raw links", len(links))

	// ── FINAL ─────────────────────────────────────────────────────────────────
	result.InternalLinks = parser.CleanLinks(links)

	if content == "" && len(result.InternalLinks) == 0 {
		return result, errors.New("no content or links extracted")
	}

	return result, nil
}
