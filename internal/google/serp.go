package google

import (
	"fmt"
	"os"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Result represents a single organic Google search result.
type Result struct {
	Title      string `json:"title"`
	URL        string `json:"url"`
	DisplayURL string `json:"display_url,omitempty"`
	SiteName   string `json:"site_name,omitempty"`
	Snippet    string `json:"snippet,omitempty"`
}

// BlockedDomains contains video/streaming platforms whose pages are
// skipped at fetch time. Domains appear in search results but are not
// auto-fetched.
var BlockedDomains = []string{
	"youtube.com",
	"youtu.be",
	"tiktok.com",
	"netflix.com",
	"vimeo.com",
	"dailymotion.com",
	"twitch.tv",
	"instagram.com",
}

// resultSelectorPair pairs a container selector with its organic filter.
// The container selector locates result blocks; the filter selector
// determines whether a block is organic (not an ad/widget).
type resultSelectorPair struct {
	container string
	filter    string
}

// resultSelectorChain defines the fallback order for SERP parsing.
// Each pair is tried in order; the first to yield results wins.
var resultSelectorChain = []resultSelectorPair{
	{"div.MjjYud", "div.wHYlTd, div.tF2Cxc"}, // Current Google (2023+)
	{"div.g", "div.rc, div.s"},                // Legacy Google (~2010-2022)
	{"div[data-sokoban-container]", "h3"},     // Semantic attribute fallback
}

// ParseResults extracts organic search results from Google SERP HTML.
// It tries multiple selector chains in order, falling back to a generic
// link collector if nothing matches.
func ParseResults(html string) ([]Result, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("google: parse HTML: %w", err)
	}

	var results []Result
	var usedSelector string

	// Try each selector pair in order
	for _, pair := range resultSelectorChain {
		results = tryParseWithSelector(doc, pair)
		if len(results) > 0 {
			usedSelector = pair.container
			break
		}
	}

	// Ultimate fallback: collect all external links
	if len(results) == 0 {
		fmt.Fprintf(os.Stderr, "[search-mcp] SERP parse: primary selectors yielded no results, using ultimate fallback\n")
		results = tryParseUltimateFallback(doc)
		if len(results) > 0 {
			usedSelector = "ultimate-fallback"
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("google: no organic results found in SERP HTML (%d bytes)", len(html))
	}

	if usedSelector != "" && usedSelector != resultSelectorChain[0].container {
		fmt.Fprintf(os.Stderr, "[search-mcp] SERP parse: matched selector %q (primary %q did not match)\n", usedSelector, resultSelectorChain[0].container)
	}

	return results, nil
}

// tryParseWithSelector attempts to extract results using a single selector pair.
func tryParseWithSelector(doc *goquery.Document, pair resultSelectorPair) []Result {
	var results []Result

	containers := doc.Find(pair.container)
	if containers.Length() == 0 {
		return nil
	}

	containers.Each(func(_ int, s *goquery.Selection) {
		// Filter: ensure container has organic content marker
		if pair.filter != "" && s.Find(pair.filter).Length() == 0 {
			return // ad, widget, or empty container — skip
		}

		r := Result{
			Title:      extractTitle(s),
			URL:        extractURL(s),
			DisplayURL: extractDisplayURL(s),
			SiteName:   extractSiteName(s),
			Snippet:    extractSnippet(s),
		}

		// Only include if we have at least a title and URL
		if r.Title != "" && r.URL != "" {
			results = append(results, r)
		}
	})

	return results
}

// tryParseUltimateFallback collects all external links as a last resort.
// Filters google.com links, fragment-only links, and deduplicates by URL.
func tryParseUltimateFallback(doc *goquery.Document) []Result {
	var results []Result
	seen := make(map[string]bool)

	doc.Find("a[href^='http']").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}

		// Skip Google internal links
		if strings.Contains(href, "google.com") {
			return
		}
		// Skip fragment-only links
		if strings.HasPrefix(href, "#") {
			return
		}

		if seen[href] {
			return
		}
		seen[href] = true

		title := strings.TrimSpace(s.Text())
		if title == "" || len(title) < 3 {
			return
		}

		results = append(results, Result{
			Title: title,
			URL:   cleanURL(href),
		})
	})

	return results
}

func extractTitle(s *goquery.Selection) string {
	return strings.TrimSpace(s.Find("h3").First().Text())
}

func extractURL(s *goquery.Selection) string {
	// Primary: a.zReHs — Google's designated result link class
	url, ok := s.Find("a.zReHs").First().Attr("href")
	if ok && url != "" && !strings.Contains(url, "google.com/search") {
		return cleanURL(url)
	}

	// Fallback 1: a[jsname] — Google's JS-driven links
	url, ok = s.Find("a[jsname]").First().Attr("href")
	if ok && url != "" && !strings.Contains(url, "google.com/search") {
		return cleanURL(url)
	}

	// Fallback 2: a[data-ved] — legacy Google tracking attribute
	url, ok = s.Find("a[data-ved]").First().Attr("href")
	if ok && url != "" && !strings.Contains(url, "google.com/search") {
		return cleanURL(url)
	}

	// Fallback: find first external link
	var fallback string
	s.Find("a[href^='http']").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href, exists := a.Attr("href")
		if !exists {
			return true
		}
		if strings.Contains(href, "google.com") {
			return true
		}
		if strings.Contains(href, "#:~:text=") {
			return true
		}
		fallback = href
		return false
	})

	return cleanURL(fallback)
}

func extractDisplayURL(s *goquery.Selection) string {
	cite := strings.TrimSpace(s.Find("cite").First().Text())
	return cite
}

func extractSiteName(s *goquery.Selection) string {
	return strings.TrimSpace(s.Find("span.VuuXrf").First().Text())
}

func extractSnippet(s *goquery.Selection) string {
	snippet := strings.TrimSpace(s.Find("div.VwiC3b, span.VwiC3b").First().Text())
	snippet = strings.TrimSuffix(snippet, "Read more")
	snippet = strings.TrimSpace(snippet)
	return snippet
}

// cleanURL removes tracking parameters and trailing junk.
func cleanURL(raw string) string {
	if raw == "" {
		return ""
	}
	// Strip Google redirect wrapper
	// Sometimes Google wraps URLs, but modern SERP gives clean URLs directly via zReHs
	return strings.TrimSpace(raw)
}

// SearchURL builds a Google search URL with the given query and options.
// Set site to restrict results to a domain (uses site: operator).
// Set num to request a specific count (note: Google ignores this parameter in modern UI).
func SearchURL(query string, site string, num int) string {
	return SearchURLWithStart(query, site, num, 0)
}

// SearchURLWithStart builds a Google search URL with pagination offset.
// start=0 for first page, start=10 for second, etc.
// Blocked domains (video/streaming) are filtered at fetch time, not excluded from search.
func SearchURLWithStart(query string, site string, num int, start int) string {
	q := query
	if site != "" {
		q = fmt.Sprintf("site:%s %s", site, q)
	}
	u := fmt.Sprintf("https://www.google.com/search?q=%s&hl=en", strings.ReplaceAll(q, " ", "+"))
	if num > 0 {
		u += fmt.Sprintf("&num=%d", num)
	}
	if start > 0 {
		u += fmt.Sprintf("&start=%d", start)
	}
	return u
}
