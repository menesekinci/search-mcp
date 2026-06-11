package google

import (
	"fmt"
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

// VideoSites is the global blocklist of video / streaming platforms.
// Appended as `-site:domain` exclusions to every Google query so that
// YouTube, TikTok, Netflix etc. never appear in results.
var VideoSites = []string{
	"youtube.com",
	"youtu.be",
	"tiktok.com",
	"netflix.com",
	"vimeo.com",
	"dailymotion.com",
	"twitch.tv",
	"instagram.com",
	"facebook.com",
	"twitter.com",
	"x.com",
	"reddit.com",
}

// appendExclusions builds a copy of query with `-site:domain` for each
// blocked video site. The original query string is not modified.
func appendExclusions(query string) string {
	for _, site := range VideoSites {
		query += " -site:" + site
	}
	return query
}

// ParseResults extracts organic search results from Google SERP HTML.
func ParseResults(html string) ([]Result, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("google: parse HTML: %w", err)
	}

	var results []Result

	doc.Find("div.MjjYud").Each(func(_ int, s *goquery.Selection) {
		// Filter: organic results have div.wHYlTd or div.tF2Cxc inside
		if s.Find("div.wHYlTd, div.tF2Cxc").Length() == 0 {
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

	return results, nil
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
			return true // "Read more" fragment links
		}
		fallback = href
		return false
	})

	return cleanURL(fallback)
}

func extractDisplayURL(s *goquery.Selection) string {
	// cite element contains the breadcrumb URL
	cite := strings.TrimSpace(s.Find("cite").First().Text())
	return cite
}

func extractSiteName(s *goquery.Selection) string {
	return strings.TrimSpace(s.Find("span.VuuXrf").First().Text())
}

func extractSnippet(s *goquery.Selection) string {
	snippet := strings.TrimSpace(s.Find("div.VwiC3b, span.VwiC3b").First().Text())
	// Remove trailing "Read more" if present
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
// Video platforms are always excluded via `-site:domain` operators.
func SearchURLWithStart(query string, site string, num int, start int) string {
	q := appendExclusions(query)
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
