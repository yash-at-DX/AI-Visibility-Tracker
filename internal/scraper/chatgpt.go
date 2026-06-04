package scraper

import (
	"context"
	"errors"
	"log"
	"net/url"
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
// /backend-api/conversation       – logged-in users
// /backend-anon/f/conversation    – anonymous sessions
// (?:\?|$) excludes /conversation/prepare (small JSON ping, not the SSE).
var chatgptConvRe = regexp.MustCompile(`chatgpt\.com/backend-(api|anon/f)/conversation(?:\?|$)`)

// chatgptCitationRe extracts citation URLs from the raw SSE body.
// ChatGPT tags every citation URL with utm_source=chatgpt or utm_source=openai.
// This pattern survives any JSON schema changes to the delta_encoding v1 format.
var chatgptCitationRe = regexp.MustCompile(`https?://[^\s"\\<>']+utm_source=(?:chatgpt|openai)[^\s"\\<>']*`)

// parseChatGPTSSE extracts citation URLs from the raw SSE body.
func parseChatGPTSSE(body []byte) (links []string) {
	raw := string(body)

	// Debug: log first 300 chars
	preview := raw
	if len(preview) > 300 {
		preview = preview[:300]
	}
	log.Printf("ChatGPT SSE preview: %q", preview)

	seen := make(map[string]bool)
	for _, m := range chatgptCitationRe.FindAllString(raw, -1) {
		u := strings.ReplaceAll(m, `\/`, `/`)
		u = strings.TrimRight(u, `.,;:!?)"`)
		if seen[u] {
			continue
		}
		seen[u] = true
		links = append(links, u)
	}
	return links
}

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

	// ── STEP 1: register CDP listener BEFORE navigation ───────────────────────
	cdpCh := RegisterCDPCapture(ctx, chatgptConvRe, 5, chromedp.ListenTarget)

	// ── STEP 2: navigate directly with query + search hint ────────────────────
	//
	// ChatGPT supports URL parameters:
	//   ?q=<query>      pre-fills and auto-submits the query
	//   ?hints=search   pre-enables web search mode
	//
	// This completely bypasses the fragile UI toggle (+ menu → Web search).
	// The SSE stream fires automatically after the page loads.
	log.Println("ChatGPT: Navigate with query")

	searchURL := "https://chatgpt.com/?hints=search&q=" + url.QueryEscape(query)

	err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(searchURL),
	)
	if err != nil {
		return result, err
	}

	log.Println("ChatGPT: waiting for SSE stream (max 90s)")

	// ── STEP 3: collect CDP response ──────────────────────────────────────────
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
		case <-deadline:
			log.Printf("ChatGPT CDP: collection window closed  links=%d",
				len(cdpLinks))
			break collect
		}
	}

	// ── STEP 4: DOM fallback ──────────────────────────────────────────────────
	var content string
	var links []string

	if len(cdpLinks) > 0 {
		links = cdpLinks
	} else {
		log.Println("ChatGPT: CDP got no links — falling back to DOM extraction")

		time.Sleep(3 * time.Second)

		stableCount := 0
		var lastLen int
		var lastContent string

		for i := 0; i < 20; i++ {
			var current string
			chromedp.Run(ctx, chromedp.Evaluate(`(() => {
				let main = document.querySelector('main');
				if (!main) return "";
				let msgs = main.querySelectorAll('[data-message-author-role="assistant"]');
				if (msgs.length) return msgs[msgs.length - 1].innerText.trim();
				return main.innerText.trim();
			})()`, &current))
			lastContent = current
			currLen := len(current)
			if currLen > 20 && currLen == lastLen {
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
		if content == "" {
			content = lastContent
		}

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

		log.Printf("ChatGPT DOM fallback: content=%d chars  links=%d",
			len(content), len(links))
	}

	log.Printf("ChatGPT: final links=%d", len(links))

	result.InternalLinks = parser.CleanLinks(links)

	if content == "" && len(result.InternalLinks) == 0 {
		return result, errors.New("no content or links extracted")
	}

	return result, nil
}
