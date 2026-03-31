package browser

import (
	"context"
	"log"
	"time"

	"github.com/chromedp/chromedp"
)

type Browser struct {
	AllocatorCtx context.Context
	Cancel       context.CancelFunc
}

func NewBrowser(headless bool) *Browser {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],

		// core
		chromedp.Flag("headless", headless),
		chromedp.Flag("start-maximized", true),

		// stability
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),

		// anti-detection
		chromedp.Flag("disable-blink-features", "AutomationControlled"),

		// performance
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-infobars", true),

		chromedp.Flag("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"),

		// session
		// chromedp.UserDataDir("./chrome-profile"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)

	return &Browser{
		AllocatorCtx: allocCtx,
		Cancel:       cancel,
	}
}

func (b *Browser) NewContext() (context.Context, context.CancelFunc) {
	ctx, cancel := chromedp.NewContext(b.AllocatorCtx, chromedp.WithLogf(log.Printf))

	ctx, timeoutCancel := context.WithTimeout(ctx, 120*time.Second)

	return ctx, func() {
		timeoutCancel()
		cancel()
	}
}

func (b *Browser) Close() {
	if b.Cancel != nil {
		b.Cancel()
		log.Println("Browser closed cleanly")
	}
}
