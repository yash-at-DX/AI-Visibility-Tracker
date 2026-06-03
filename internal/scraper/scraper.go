package scraper

import (
	"context"
	"log"
	"regexp"
	"sync"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/yash-at-DX/ai-scraper/internal/models"
)

// Scraper is the common interface every platform scraper must implement.
type Scraper interface {
	Scrape(ctx context.Context, query string) (models.Result, error)
	Name() string
}

// CDPBody is a single captured HTTP response body together with its URL.
type CDPBody struct {
	URL  string
	Body []byte
}

// RegisterCDPCapture attaches a network listener and returns a buffered channel
// that receives every response body whose URL matches urlPattern.
//
// listenerFn controls whether the listener is tab-level or browser-level:
//   - chromedp.ListenTarget  — tab-level (default for Google AI, Gemini, ChatGPT)
//     Bound to the current tab; survives in-tab XHR/fetch but NOT cross-tab navigations.
//   - chromedp.ListenBrowser — browser-level (required for Perplexity)
//     Perplexity navigates to a new URL on submit (perplexity.ai → perplexity.ai/search/...)
//     which resets the tab context. ListenBrowser survives across navigations.
//
// Usage:
//
//	ch := RegisterCDPCapture(ctx, re, 10, chromedp.ListenTarget)   // most platforms
//	ch := RegisterCDPCapture(ctx, re, 10, chromedp.ListenBrowser)  // Perplexity
func RegisterCDPCapture(
	ctx context.Context,
	urlPattern *regexp.Regexp,
	bufSize int,
	listenFn func(ctx context.Context, fn func(ev interface{})),
) <-chan CDPBody {
	if bufSize <= 0 {
		bufSize = 10
	}
	ch := make(chan CDPBody, bufSize)

	var (
		mu     sync.Mutex
		wanted = make(map[network.RequestID]string)
	)

	listenFn(ctx, func(ev interface{}) {
		switch e := ev.(type) {

		// Phase 1 — record request IDs whose URLs match our pattern.
		case *network.EventResponseReceived:
			switch e.Type {
			case network.ResourceTypeXHR,
				network.ResourceTypeFetch,
				network.ResourceTypeDocument,
				network.ResourceTypeEventSource:
			default:
				return
			}
			if !urlPattern.MatchString(e.Response.URL) {
				return
			}
			log.Printf("CDPCapture: tracking %s", e.Response.URL)
			mu.Lock()
			wanted[e.RequestID] = e.Response.URL
			mu.Unlock()

		// Phase 2 — fetch body once Chrome has finished receiving it.
		// For SSE streams, loadingFinished fires when the server closes
		// the connection (after data: [DONE] or when the stream ends).
		case *network.EventLoadingFinished:
			mu.Lock()
			u, ok := wanted[e.RequestID]
			if ok {
				delete(wanted, e.RequestID)
			}
			mu.Unlock()
			if !ok {
				return
			}

			// CRITICAL: never call .Do(ctx) on the listener goroutine — deadlock.
			go func(reqID network.RequestID, u string) {
				c := chromedp.FromContext(ctx)
				ectx := cdp.WithExecutor(ctx, c.Target)

				body, err := network.GetResponseBody(reqID).Do(ectx)
				if err != nil {
					log.Printf("CDPCapture: GetResponseBody(%s): %v", u, err)
					return
				}

				log.Printf("CDPCapture: captured %d bytes from %s", len(body), u)

				select {
				case ch <- CDPBody{URL: u, Body: body}:
				default:
					log.Printf("CDPCapture: channel full, dropping %s", u)
				}
			}(e.RequestID, u)
		}
	})

	return ch
}
