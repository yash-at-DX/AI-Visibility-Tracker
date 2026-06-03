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

// ── pre-compiled regexps ─────────────────────────────────────────────────────

// googleAsyncRe matches the async endpoint that carries the full AI Overview
// HTML/ADL payload.
//
// Confirmed from live traffic (June 2026, udm=50 AI Mode):
//
//	/async/folwr    – primary response, always fires first, ~500–700 KB
//	                 (_fmt:adl, contains full answer text + citation anchors)
//	/async/folsrch  – older variant of the same endpoint, same payload format
//
// Intentionally excluded:
//
//	/async/hpba     – JS/CSS loader manifest, ~100 bytes, no AI content
//	/async/bgasy    – page config blob, ~8 KB, no AI content
//
// In practice folwr fires on every query tested. folsrch is the fallback
// pattern in case Google A/B tests the endpoint name back.
var googleAsyncRe = regexp.MustCompile(`google\.com/async/(folwr|folsrch)`)

// htmlTagRe strips every HTML tag so we get readable plain text.
var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

// whitespaceRe collapses runs of whitespace into a single space.
var whitespaceRe = regexp.MustCompile(`\s+`)

// hrefRe extracts href attribute values from raw HTML.
// We look specifically for http/https values — no relative URLs.
var hrefRe = regexp.MustCompile(`href="(https?://[^"]+)"`)

// ── scraper ──────────────────────────────────────────────────────────────────

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

	// ── STEP 1: register CDP listener BEFORE navigation ──────────────────────
	//
	// RegisterCDPCapture (defined in scraper.go) wires up a network.Enable
	// listener and delivers every matching response body to the returned
	// channel.  It must be called before chromedp.Run so it is active for
	// the very first request the page makes.
	//
	// When CDP is extended to ChatGPT/Perplexity/Gemini, those scrapers call
	// RegisterCDPCapture with their own urlPattern and handle the channel
	// bodies in their own parse functions — everything else is identical.
	asyncCh := RegisterCDPCapture(ctx, googleAsyncRe, 10, chromedp.ListenTarget)

	// ── STEP 2: navigate ─────────────────────────────────────────────────────
	//
	// &udm=50 = Google AI Mode
	searchURL := "https://www.google.com/search?q=" +
		url.QueryEscape(query) + "&udm=50"

	log.Println("Google AI: navigate →", searchURL)

	err := chromedp.Run(ctx,
		network.Enable(), // MUST be the first action — enables CDP event stream
		chromedp.Navigate(searchURL),
	)
	if err != nil {
		return result, err
	}

	// ── STEP 3: collect async fragments ─────────────────────────────────────
	//
	// Google fires the /async/folsrch (or similar) request 1–5 seconds after
	// the main page loads.  We give it 20 seconds, which covers slow
	// connections and queries where the AI model takes longer to respond.
	//
	// Google can split the AI Overview across multiple async requests.
	// We collect everything and pick the largest body, which is the full
	// AI overview rather than a loading skeleton or partial update.

	log.Println("Google AI: waiting for async fragments (max 20s)…")

	var fragments []CDPBody
	deadline := time.After(20 * time.Second)

collect:
	for {
		select {
		case body := <-asyncCh:
			fragments = append(fragments, body)
			log.Printf("Google AI CDP: fragment %d  url=%s  size=%d bytes",
				len(fragments), body.URL, len(body.Body))

		case <-deadline:
			log.Printf("Google AI CDP: collection window closed  fragments=%d",
				len(fragments))
			break collect
		}
	}

	// ── STEP 4: parse the best fragment ──────────────────────────────────────
	var content string
	var links []string

	if len(fragments) > 0 {
		// Use the largest fragment — it has the most complete AI overview.
		best := fragments[0]
		for _, f := range fragments[1:] {
			if len(f.Body) > len(best.Body) {
				best = f
			}
		}

		log.Printf("Google AI CDP: parsing best fragment  url=%s  size=%d",
			best.URL, len(best.Body))

		content, links = parseGoogleFragment(best.Body)

		log.Printf("Google AI CDP: content=%d chars  links=%d",
			len(content), len(links))
	}

	// ── STEP 5: DOM fallback ─────────────────────────────────────────────────
	//
	// If CDP captured nothing (the endpoint URL changed, or the async request
	// fired before network.Enable() was acknowledged on first run), fall back
	// to DOM-based extraction so the scrape is not a complete loss.
	//
	// Remove this block once CDP has been confirmed stable across ≥ 3 cron
	// runs in production.

	if content == "" && len(links) == 0 {
		log.Println("Google AI: CDP captured nothing — falling back to DOM extraction")
		time.Sleep(3 * time.Second) // give the page extra time to render
		content, links = googleDOMFallback(ctx)
		log.Printf("Google AI DOM fallback: content=%d chars  links=%d",
			len(content), len(links))
	}

	// ── FINAL ─────────────────────────────────────────────────────────────────
	result.InternalLinks = parser.CleanLinks(links)

	if content == "" {
		if len(result.InternalLinks) > 0 {
			// Links without text is still useful for the dashboard.
			log.Println("Google AI: no content but has citation links — saving")
			return result, nil
		}
		return result, errors.New("no content or links extracted")
	}

	return result, nil
}

// ── parseGoogleFragment ───────────────────────────────────────────────────────
//
// Parses the _fmt:adl response that Google delivers via /async/folwr.
//
// The ADL (Async Data Layer) format is a ~500KB concatenation of HTML chunks
// and JS update blobs.  Naively stripping all tags produces 170K+ chars of
// garbage.  Instead we:
//  1. Locate the HTML segment that contains the AI answer (identified by
//     known answer-container class names used as locators only).
//  2. Extract text from a 60 KB window around that segment.
//  3. Extract all external href values from the full raw body (class-name
//     independent — citation links appear regardless of class renames).
//
// adlAnswerStartRe locates the opening tag of the AI answer container div.
// Used as a positional anchor only — class names here are locators, not
// selectors.  Link extraction never uses these names.
var adlAnswerStartRe = regexp.MustCompile(`(?i)<div[^>]+class="[^"]*(?:n6owBd|LangJde|wDYxhc|IVvmDb|pWvJNd)[^"]*"`)

// divOpenRe and divCloseRe are used by extractDivBlock to track nesting depth.
var divOpenRe = regexp.MustCompile(`(?i)<div[\s>]`)
var divCloseRe = regexp.MustCompile(`(?i)</div>`)

// extractDivBlock finds the opening tag matched by anchor in raw, then walks
// forward counting <div> opens and </div> closes to find the matching closing
// tag.  Returns the full HTML of that div block.
// This is more reliable than a fixed byte window or JS-boundary heuristics
// because it follows the actual HTML structure.
func extractDivBlock(raw string, anchor *regexp.Regexp) string {
	loc := anchor.FindStringIndex(raw)
	if loc == nil {
		return ""
	}
	start := loc[0]
	pos := loc[1] // start scanning after the opening tag
	depth := 1    // we are inside the anchor div

	for pos < len(raw) && depth > 0 {
		// Find the next <div or </div from current position.
		nextOpen := divOpenRe.FindStringIndex(raw[pos:])
		nextClose := divCloseRe.FindStringIndex(raw[pos:])

		hasOpen := nextOpen != nil
		hasClose := nextClose != nil

		if !hasOpen && !hasClose {
			break // no more div tags — take everything to end
		}

		var openPos, closePos int
		if hasOpen {
			openPos = pos + nextOpen[0]
		}
		if hasClose {
			closePos = pos + nextClose[0]
		}

		// Whichever comes first, process it.
		if hasOpen && (!hasClose || openPos < closePos) {
			depth++
			pos = pos + nextOpen[1]
		} else {
			depth--
			pos = pos + nextClose[1]
			if depth == 0 {
				return raw[start:pos]
			}
		}
	}

	// depth never reached 0 — return everything from start (truncated at 80 KB)
	end := start + 80000
	if end > len(raw) {
		end = len(raw)
	}
	return raw[start:end]
}

func parseGoogleFragment(body []byte) (content string, links []string) {
	raw := string(body)

	// ── extract answer text using div-depth tracking ──────────────────────────
	//
	// extractDivBlock finds the exact opening and closing tags of the AI answer
	// container by counting nested divs.  This gives us only the answer HTML —
	// no surrounding page scaffolding, no ADL JS chunks, no other sections.
	// Typical result: 5–20 KB of HTML → 1,000–8,000 chars of plain text.

	answerHTML := extractDivBlock(raw, adlAnswerStartRe)
	if answerHTML != "" {
		text := htmlTagRe.ReplaceAllString(answerHTML, " ")
		text = whitespaceRe.ReplaceAllString(text, " ")
		text = strings.TrimSpace(text)
		if len(text) >= 80 {
			content = text
		}
	}

	// ── extract citation links from full body ─────────────────────────────────
	// href matching is class-name independent — citation anchors appear
	// throughout the ADL body regardless of class name changes.
	seen := make(map[string]bool)
	skipDomains := []string{
		"google.com",
		"accounts.google",
		"gstatic.com",
		"youtube.com",
		"googleusercontent.com",
	}

	for _, m := range hrefRe.FindAllStringSubmatch(raw, -1) {
		if len(m) < 2 {
			continue
		}
		u := m[1]
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

	return content, links
}

// ── googleDOMFallback ─────────────────────────────────────────────────────────
//
// DOM-based extraction kept as a safety net while CDP is being validated.
// Uses the same selector lists as the pre-CDP implementation.
// Remove once CDP is confirmed stable in production.

func googleDOMFallback(ctx context.Context) (content string, links []string) {
	// Try content selectors newest-first.
	contentJS := `(() => {
		let sels = [
			".n6owBd", ".awi2gc", ".jKhXsc", ".wDYxhc",
			".pWvJNd", ".IVvmDb", ".LGOjhe", ".vxQmIe"
		];
		for (let sel of sels) {
			let els = document.querySelectorAll(sel);
			if (!els.length) continue;
			let text = Array.from(els).map(e => e.innerText).join("\n").trim();
			if (text.length > 30) return text;
		}
		return "";
	})()`

	chromedp.Run(ctx, chromedp.Evaluate(contentJS, &content))

	// Citation links — a.NDNGvf confirmed present in live HTML (June 2026).
	for _, sel := range []string{`a.NDNGvf`, `.EJw9bc a.NDNGvf`, `.bTFeG a.NDNGvf`} {
		js := `(() => {
			return Array.from(document.querySelectorAll('` + sel + `'))
				.map(a => a.href)
				.filter(h => h && h.startsWith("http") && !h.includes("google.com"));
		})()`
		chromedp.Run(ctx, chromedp.Evaluate(js, &links))
		if len(links) > 0 {
			log.Printf("Google AI DOM fallback: %d links via %s", len(links), sel)
			break
		}
	}

	return content, links
}
