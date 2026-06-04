package scraper

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/yash-at-DX/ai-scraper/internal/browser"
	"github.com/yash-at-DX/ai-scraper/internal/models"
	"github.com/yash-at-DX/ai-scraper/internal/parser"
)

// geminiStreamRe matches Gemini's main answer stream endpoint.
//
// Note on anonymous Gemini citations:
// When queries are submitted with "with sources" appended (see Scrape()),
// anonymous Gemini performs web grounding and returns citation links in the
// DOM via a Sources sidebar panel (🔗 Sources button in the response).
// The StreamGenerate response body only contains UI font assets, not URLs,
// so we rely on the DOM fallback for citation extraction.
// CDP capture here is kept only to confirm the stream completed.
var geminiStreamRe = regexp.MustCompile(`BardFrontendService/StreamGenerate`)

// geminiHrefRe matches plain http/https URLs after the body is unescaped.
// Stops at whitespace, quotes, backslashes, brackets, and braces.
var geminiHrefRe = regexp.MustCompile(`https?://[^\s"\\<>\]\[}{)]+`)

// parseGeminiStream extracts citation URLs from the raw Gemini stream body.
//
// Gemini wraps content in multiply-encoded JSON: outer array → JSON-encoded
// inner string → JSON-encoded another inner string → eventually URLs.
// Rather than navigating the structure, we iteratively unescape the entire
// body until URLs become plain text, then regex-match them.
func parseGeminiStream(body []byte) (links []string) {
	raw := string(body)

	// Strip the )]}' anti-XSSI prefix if present (first line is length marker)
	if idx := strings.Index(raw, "\n"); idx != -1 {
		raw = raw[idx+1:]
	}

	// Iteratively unescape until the body stops changing.
	// Handles multi-level nesting (\\\" → \" → ") in one loop.
	prev := ""
	unescaped := raw
	for i := 0; i < 6 && unescaped != prev; i++ {
		prev = unescaped
		unescaped = strings.ReplaceAll(unescaped, `\/`, `/`)
		unescaped = strings.ReplaceAll(unescaped, `\"`, `"`)
		unescaped = strings.ReplaceAll(unescaped, `\\`, `\`)
	}

	seen := make(map[string]bool)
	skipDomains := []string{
		"google.com",
		"googleapis.com",
		"gstatic.com",
		"youtube.com",
		"googleusercontent.com",
		"gemini.google",
	}

	for _, m := range geminiHrefRe.FindAllString(unescaped, -1) {
		u := strings.TrimRight(m, `.,;:!?)"\`)
		if len(u) < 12 || !strings.Contains(u, ".") {
			continue
		}
		skip := false
		for _, domain := range skipDomains {
			if strings.Contains(u, domain) {
				skip = true
				break
			}
		}
		if skip || seen[u] {
			continue
		}
		seen[u] = true
		links = append(links, u)
	}

	return links
}

// ── scraper ───────────────────────────────────────────────────────────────────

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

	// ── STEP 1: register CDP listener BEFORE navigation ───────────────────────
	cdpCh := RegisterCDPCapture(ctx, geminiStreamRe, 10, chromedp.ListenTarget)

	// ── STEP 2: navigate ──────────────────────────────────────────────────────
	log.Println("Gemini: Navigate")

	err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate("https://gemini.google.com/"),
	)
	if err != nil {
		return result, err
	}

	time.Sleep(6 * time.Second)

	// ── STEP 3: type and submit query ─────────────────────────────────────────
	log.Println("Gemini: Typing query")

	time.Sleep(2 * time.Second)

	chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		let el = document.querySelector('.ql-editor') ||
		         document.querySelector('[contenteditable="true"]') ||
		         document.querySelector('rich-textarea');
		if (el) { el.click(); el.focus(); return true; }
		return false;
	})()`, nil))

	time.Sleep(1 * time.Second)

	// Append "with sources" so Gemini performs web grounding and returns
	// citation links even for anonymous sessions.
	queryWithSources := query + " with sources"

	err = chromedp.Run(ctx,
		chromedp.Evaluate(
			fmt.Sprintf(`document.execCommand('insertText', false, %q);`, queryWithSources),
			nil,
		),
	)
	if err != nil {
		err = chromedp.Run(ctx,
			chromedp.Evaluate(fmt.Sprintf(`(() => {
				let el = document.querySelector('.ql-editor') ||
				         document.querySelector('[contenteditable="true"]');
				if (!el) return false;
				el.focus();
				el.innerText = %q;
				el.dispatchEvent(new Event('input', { bubbles: true }));
				return true;
			})()`, queryWithSources), nil),
		)
		if err != nil {
			return result, errors.New("failed to insert text")
		}
	}

	time.Sleep(500 * time.Millisecond)

	err = chromedp.Run(ctx, chromedp.KeyEvent("\n"))
	if err != nil {
		return result, err
	}

	log.Println("Gemini: Query submitted — waiting for stream (max 60s)")

	// ── STEP 4: collect CDP response ──────────────────────────────────────────
	var cdpLinks []string
	seenURLs := make(map[string]bool)
	deadline := time.After(60 * time.Second)

collect:
	for {
		select {
		case body := <-cdpCh:
			log.Printf("Gemini CDP: captured %d bytes from %s",
				len(body.Body), body.URL)
			for _, u := range parseGeminiStream(body.Body) {
				if !seenURLs[u] {
					seenURLs[u] = true
					cdpLinks = append(cdpLinks, u)
				}
			}
			log.Printf("Gemini CDP: running total links=%d", len(cdpLinks))
		case <-deadline:
			log.Printf("Gemini CDP: collection window closed  links=%d",
				len(cdpLinks))
			break collect
		}
	}

	// ── STEP 5: DOM fallback ──────────────────────────────────────────────────
	var content string
	var links []string

	if len(cdpLinks) > 0 {
		links = cdpLinks
	} else {
		log.Println("Gemini: CDP got no links — falling back to DOM extraction")

		contentJS := `(() => {
			let sels = [
				'.markdown-main-panel', '.response-content',
				'model-response', '[data-test-id="response"]',
				'[class*="ModelResponse"]', '[class*="response-text"]'
			];
			for (let sel of sels) {
				let els = document.querySelectorAll(sel);
				if (!els.length) continue;
				let text = els[els.length - 1].innerText.trim();
				if (text.length > 50) return text;
			}
			return "";
		})()`

		stableCount := 0
		var lastLen int
		for i := 0; i < 15; i++ {
			var current string
			chromedp.Run(ctx, chromedp.Evaluate(contentJS, &current))
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
			if currentLen > 1200 {
				content = current
				break
			}
			time.Sleep(2 * time.Second)
		}

		chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			let btn = document.querySelector('button[aria-label*="source"]') ||
			          document.querySelector('button[aria-label*="Source"]');
			if (btn) { btn.click(); return; }
			let btns = Array.from(document.querySelectorAll('button'));
			let numbered = btns.find(b => b.innerText && /^\d+$/.test(b.innerText.trim()));
			if (numbered) { numbered.click(); return; }
			let textBtn = btns.find(b =>
				b.innerText && b.innerText.toLowerCase().includes("source"));
			if (textBtn) textBtn.click();
		})()`, nil))
		time.Sleep(3 * time.Second)

		chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			let links = new Set();
			let panel = document.querySelector('context-sidebar') ||
			            document.querySelector('[class*="sidebar"]');
			let scope = panel || document;
			scope.querySelectorAll('a[href]').forEach(a => {
				if (a.href && a.href.startsWith('http') &&
				    !a.href.includes('google.com') &&
				    !a.href.includes('gemini.google'))
					links.add(a.href);
			});
			return Array.from(links);
		})()`, &links))

		log.Printf("Gemini DOM fallback: content=%d chars  links=%d",
			len(content), len(links))
	}

	log.Printf("Gemini: final links=%d", len(links))

	result.InternalLinks = parser.CleanLinks(links)

	if content == "" && len(result.InternalLinks) == 0 {
		return result, errors.New("no content or links extracted")
	}

	return result, nil
}
