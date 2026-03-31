// package parser

// import (
// 	"strings"
// )

// func CleanLinks(links []string) []string {
// 	seen := make(map[string]bool)
// 	var cleaned []string

// 	for _, l := range links {
// 		if l == "" {
// 			continue
// 		}
// 		if strings.Contains(l, "perplexity.ai") {
// 			continue
// 		}
// 		if !seen[l] {
// 			seen[l] = true
// 			cleaned = append(cleaned, l)
// 		}
// 	}
// 	return cleaned
// }

package parser

import (
	"net/url"
	"strings"
)

func CleanLinks(links []string) []string {
	seen := make(map[string]bool)
	var cleaned []string

	for _, l := range links {
		if l == "" {
			continue
		}

		l = strings.TrimSpace(l)

		// parse URL
		parsed, err := url.Parse(l)
		if err != nil {
			continue
		}

		host := parsed.Host

		// ❌ remove ONLY if actual domain is chatgpt/openai/perplexity
		if strings.Contains(host, "chatgpt.com") ||
			strings.Contains(host, "openai.com") ||
			strings.Contains(host, "perplexity.ai") {
			continue
		}

		// ❌ remove invalid schemes
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			continue
		}

		// ✅ remove tracking params ONLY
		q := parsed.Query()
		q.Del("utm_source")
		q.Del("utm_medium")
		q.Del("utm_campaign")
		q.Del("utm_term")
		q.Del("utm_content")
		q.Del("utm_id")
		q.Del("gclid")
		q.Del("fbclid")

		parsed.RawQuery = q.Encode()
		l = parsed.String()

		// dedupe
		if !seen[l] {
			seen[l] = true
			cleaned = append(cleaned, l)
		}
	}

	return cleaned
}
