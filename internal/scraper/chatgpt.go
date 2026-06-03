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

// chatgptConvRe matches the SSE endpoint that carries the full ChatGPT response.
// The stream starts when the user submits a message and ends with data: [DONE].
//
// Two endpoint variants depending on session type:
//
//	/backend-api/conversation       – logged-in users
//	/backend-anon/f/conversation    – anonymous sessions (no login)
//
// The (?:\?|$) suffix ensures we match the SSE endpoint exactly, not
// related sub-paths like /conversation/prepare (a small JSON ping that
// fires before the real SSE stream).
var chatgptConvRe = regexp.MustCompile(`chatgpt\.com/backend-(api|anon/f)/conversation(?:\?|$)`)

// chatgptCitationRe extracts citation URLs from the raw SSE body.
//
// As of June 2026 ChatGPT's SSE format is "delta_encoding v1" — incremental
// patches rather than full message snapshots. Citation URLs are embedded
// in the deltas at unpredictable paths that change between versions.
//
// Instead of navigating the nested JSON, we regex-match citation URLs
// directly from the raw body. ChatGPT tags every citation URL with
// utm_source=chatgpt.com (or utm_source=openai), giving us a precise
// pattern that survives any JSON schema change.
//
// The encoded variants (utm_source=chatgpt%2Ecom) also appear because
// URLs are sometimes JSON-escaped — we handle both.
var chatgptCitationRe = regexp.MustCompile(`https?://[^\s"\\<>']+utm_source=(?:chatgpt|openai)[^\s"\\<>']*`)

// parseChatGPTSSE extracts citation URLs from the raw SSE body.
// Format-agnostic: matches URLs by their utm_source tag, not by JSON path.
func parseChatGPTSSE(body []byte) (links []string) {
	raw := string(body)

	// Debug: log first 400 chars of the SSE body
	preview := raw
	if len(preview) > 400 {
		preview = preview[:400]
	}
	log.Printf("ChatGPT SSE preview: %q", preview)

	seen := make(map[string]bool)
	for _, m := range chatgptCitationRe.FindAllString(raw, -1) {
		// Unescape backslash-escaped slashes (URLs inside JSON strings)
		u := strings.ReplaceAll(m, `\/`, `/`)
		// Trim trailing punctuation that may have been swept up
		u = strings.TrimRight(u, ".,;:!?)\"")

		if seen[u] {
			continue
		}
		seen[u] = true
		links = append(links, u)
	}

	return links
}

// ── scraper ───────────────────────────────────────────────────────────────────

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

	// ── STEP 1: navigate ────────────────────────────────────────────────────────
	log.Println("ChatGPT: Navigate")

	err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate("https://chatgpt.com/"),
	)
	if err != nil {
		return result, err
	}

	// ── STEP 2: register CDP listener AFTER navigate, BEFORE query ────────────
	//
	// ChatGPT's URL changes from chatgpt.com → chatgpt.com/c/<uuid> on submit
	// but this happens via history.pushState (React SPA), not a real navigation.
	// The tab/target stays the same, so ListenTarget works correctly.
	//
	// IMPORTANT: ListenBrowser does NOT receive network events from individual
	// tabs — only browser-level events like target creation/destruction.
	// Network events (which we need) require ListenTarget.
	//
	// The /backend-api/conversation SSE fires after submit and closes with
	// data: [DONE] — loadingFinished fires when the stream ends (30-90s).
	cdpCh := RegisterCDPCapture(ctx, chatgptConvRe, 5, chromedp.ListenTarget)

	// ── STEP 3: wait for input (retry loop) ───────────────────────────────────
	log.Println("ChatGPT: Waiting for input")

	inputFound := false
	for i := 0; i < 15; i++ {
		var found bool
		chromedp.Run(ctx, chromedp.Evaluate(
			`(() => !!document.getElementById('prompt-textarea'))()`,
			&found,
		))
		if found {
			inputFound = true
			break
		}
		// Dismiss blocking modals if present
		chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			let btns = Array.from(document.querySelectorAll('button'));
			let dismiss = btns.find(b =>
				b.innerText && /^(continue|stay logged out|ok|got it|dismiss|close)/i.test(b.innerText.trim())
			);
			if (dismiss) dismiss.click();
		})()`, nil))
		time.Sleep(2 * time.Second)
	}

	if !inputFound {
		return result, errors.New("input not found")
	}

	time.Sleep(500 * time.Millisecond)

	// ── STEP 4: enable web search ─────────────────────────────────────────────
	log.Println("ChatGPT: Enabling web search")

	var alreadyEnabled bool
	chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		return !!document.querySelector('[data-testid="search-status-icon"]') ||
		       !!document.querySelector('[aria-label*="Search is on"]') ||
		       !!document.querySelector('[data-testid="composer-search-toggle"][aria-pressed="true"]');
	})()`, &alreadyEnabled))

	if !alreadyEnabled {
		err = chromedp.Run(ctx,
			chromedp.Click(`button[data-testid="composer-plus-btn"]`, chromedp.ByQuery),
		)
		if err != nil {
			chromedp.Run(ctx,
				chromedp.Click(`button[aria-label*="Attach"]`, chromedp.ByQuery),
			)
		}
		time.Sleep(1 * time.Second)

		var status string
		chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			let items = document.querySelectorAll('[role="menuitemradio"]');
			for (let item of items) {
				if (item.innerText && item.innerText.toLowerCase().includes("web search")) {
					let btn = item.querySelector('button') || item;
					btn.click();
					return item.getAttribute("aria-checked");
				}
			}
			let searchBtns = Array.from(document.querySelectorAll('button, [role="button"]'));
			let found = searchBtns.find(b =>
				b.innerText && b.innerText.toLowerCase().includes("search") &&
				b.getAttribute("aria-pressed") !== undefined
			);
			if (found) { found.click(); return "toggled"; }
			return "not_found";
		})()`, &status))
		log.Println("ChatGPT: Web search click status:", status)

		chromedp.Run(ctx, chromedp.KeyEvent("\x1b"))
		time.Sleep(500 * time.Millisecond)
	} else {
		log.Println("ChatGPT: Web search already enabled")
	}

	time.Sleep(1 * time.Second)

	// ── STEP 5: type and submit query ─────────────────────────────────────────
	log.Println("ChatGPT: Typing query")

	typeQueryJS := fmt.Sprintf(`(() => {
		const editor = document.getElementById('prompt-textarea');
		if (!editor) return false;
		editor.focus();
		const inserted = document.execCommand('insertText', false, %q);
		if (inserted) return true;
		const target = editor.querySelector('p') || editor;
		const lastHTML = target.innerHTML;
		target.innerHTML = %q;
		['input', 'change', 'keyup'].forEach(type => {
			const ev = new Event(type, { bubbles: true });
			ev.simulated = true;
			const tracker = editor._valueTracker;
			if (tracker) tracker.setValue(lastHTML);
			editor.dispatchEvent(ev);
		});
		return true;
	})()`, query, query)

	var typed bool
	err = chromedp.Run(ctx, chromedp.Evaluate(typeQueryJS, &typed))
	if err != nil || !typed {
		return result, errors.New("failed to type query")
	}

	time.Sleep(500 * time.Millisecond)

	// Re-enable network domain after any page navigation that may have reset it.
	// ChatGPT redirects to chatgpt.com/c/<uuid> which can reset CDP network events.
	chromedp.Run(ctx, network.Enable())

	log.Println("ChatGPT: Submitting query")

	submitted := false
	for _, sel := range []string{
		`button#composer-submit-button`,
		`button[data-testid="send-button"]`,
		`button[aria-label="Send message"]`,
		`button[aria-label="Send prompt"]`,
	} {
		if chromedp.Run(ctx, chromedp.Click(sel, chromedp.ByQuery)) == nil {
			submitted = true
			break
		}
	}
	if !submitted {
		chromedp.Run(ctx,
			chromedp.Focus(`#prompt-textarea`, chromedp.ByQuery),
			chromedp.KeyEvent("\n"),
		)
	}

	log.Println("ChatGPT: Query submitted — waiting for SSE stream (max 90s)")

	// ── STEP 6: collect CDP response ──────────────────────────────────────────
	//
	// ChatGPT's SSE stream can take 30-90 seconds for a full response.
	// loadingFinished fires only after data: [DONE] — so we wait the full window.

	var cdpLinks []string
	deadline := time.After(90 * time.Second)

collect:
	for {
		select {
		case body := <-cdpCh:
			log.Printf("ChatGPT CDP: captured %d bytes from %s",
				len(body.Body), body.URL)
			parsed := parseChatGPTSSE(body.Body)
			for _, u := range parsed {
				seen := false
				for _, existing := range cdpLinks {
					if existing == u {
						seen = true
						break
					}
				}
				if !seen {
					cdpLinks = append(cdpLinks, u)
				}
			}
			log.Printf("ChatGPT CDP: parsed=%d  running total=%d",
				len(parsed), len(cdpLinks))
			// Keep collecting — ChatGPT may stream multiple bodies.
			// Only stop on timeout to avoid premature exit.
		case <-deadline:
			log.Printf("ChatGPT CDP: collection window closed  links=%d",
				len(cdpLinks))
			break collect
		}
	}

	// ── STEP 7: DOM fallback ──────────────────────────────────────────────────
	var content string
	var links []string

	if len(cdpLinks) > 0 {
		links = cdpLinks
	} else {
		log.Println("ChatGPT: CDP got no links — falling back to DOM extraction")

		// DOM fallback: wait for response to stabilise then extract from page
		time.Sleep(3 * time.Second)

		stableCount := 0
		var lastLen int
		var lastContent string

		for i := 0; i < 20; i++ {
			var current string
			getTextJS := fmt.Sprintf(`(() => {
				let main = document.querySelector('main');
				if (!main) return "";
				let msgs = main.querySelectorAll('[data-message-author-role="assistant"]');
				if (msgs.length) return msgs[msgs.length - 1].innerText.trim();
				let text = main.innerText || "";
				let query = %q;
				let idx = text.lastIndexOf(query);
				if (idx > -1) return text.substring(idx + query.length).trim();
				return text.trim();
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

		sourcesCtx, cancelSources := context.WithTimeout(ctx, 4*time.Second)
		defer cancelSources()
		chromedp.Run(sourcesCtx, chromedp.Evaluate(`(() => {
			let labels = ["Sources", "Search results", "References"];
			for (let label of labels) {
				let btn = document.querySelector('button[aria-label="' + label + '"]');
				if (btn) { btn.click(); return label; }
			}
			let btns = Array.from(document.querySelectorAll('button'));
			let found = btns.find(b => b.innerText && /sources|references/i.test(b.innerText));
			if (found) { found.click(); return "text_match"; }
			return "not_found";
		})()`, nil))
		time.Sleep(1 * time.Second)

		chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			let links = new Set();
			document.querySelectorAll('a[href*="utm_source=chatgpt"]').forEach(a => {
				if (a.href) links.add(a.href);
			});
			if (links.size === 0) {
				document.querySelectorAll('main a[href]').forEach(a => {
					if (a.href && a.href.startsWith("http") && !a.href.includes("openai"))
						links.add(a.href);
				});
			}
			return Array.from(links);
		})()`, &links))

		log.Printf("ChatGPT DOM fallback: content=%d chars  links=%d", len(content), len(links))
	}

	log.Printf("ChatGPT: final links=%d", len(links))

	result.InternalLinks = parser.CleanLinks(links)

	if content == "" && len(result.InternalLinks) == 0 {
		return result, errors.New("no content or links extracted")
	}

	return result, nil
}
