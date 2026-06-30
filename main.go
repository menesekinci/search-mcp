package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/menesekinci/search-mcp/internal/context7"
	"github.com/menesekinci/search-mcp/internal/google"
	"github.com/menesekinci/search-mcp/internal/kimi"
	"github.com/menesekinci/search-mcp/internal/mcp"
	"github.com/menesekinci/search-mcp/internal/parser"
	"github.com/menesekinci/search-mcp/internal/setup"
)

// ─── Configuration ──────────────────────────────────────────────

const Version = "v0.6.0"

var searchLevel = map[string]int{
	"low": 6, "medium": 12, "high": 24, "crazy": 48,
}

const (
	maxPageCharsSingle   = 8000  // per-page markdown in web_search
	maxPageCharsParallel = 6000  // per-page markdown in parallel mode
	maxPageCharsFetch    = 30000 // per-page markdown in fetch_page (standalone)
)

// ─── MCP tool definitions ───────────────────────────────────────

var tools = []mcp.Tool{
	{
		Name: "web_search",
		Description: `Search Google via real Chrome, then auto-fetch the top results as clean markdown.
One call = search + fetch. Always live — every call hits Google fresh, nothing is cached.

Single:  {"query": "...", "level": "high", "site": "github.com"}
Parallel: {"queries": [{"query":"...","site":"..."}, "plain string"], "level": "medium"}
+Context7: {"query": "...", "context7": true} — also queries library docs

Levels: low(6) · medium(12) · high(24) · crazy(48). Default: medium.`,
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"query":    {Type: "string", Description: "Search terms. Supports site:, filetype:, -term, \"phrase\"."},
				"queries":  {Type: "array", Description: "Multiple queries for parallel research (2-5)."},
				"level":    {Type: "string", Description: "low | medium | high | crazy"},
				"site":     {Type: "string", Description: "Restrict to domain (e.g. github.com)."},
				"context7": {Type: "boolean", Description: "Also query Context7 for library docs alongside web search."},
			},
		},
	},
	{
		Name: "fetch_page",
		Description: `Fetch a specific URL, extract main content, return clean markdown.
Use for direct links. Not needed after web_search (it auto-fetches). Always live.

{"url": "https://example.com/article"}`,
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"url": {Type: "string", Description: "Page URL."},
			},
			Required: []string{"url"},
		},
	},
}

// ─── Application ────────────────────────────────────────────────

type app struct {
	tc   *kimi.TabbedClient
	log  logFn
	ctx7 *context7.Client
}

type logFn func(format string, v ...any)

func main() {
	// Setup wizard mode
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		setup.RunWithVersion(Version)
		return
	}

	// Version flag
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("search-mcp " + Version)
		return
	}

	logf := func(format string, v ...any) {
		fmt.Fprintf(os.Stderr, "[search-mcp] "+format+"\n", v...)
	}

	app := &app{
		tc:  kimi.NewTabbedClient("search-mcp", "Search MCP"),
		log: logf,
	}

	if key := os.Getenv("CONTEXT7_API_KEY"); key != "" {
		app.ctx7 = context7.NewClient(key)
		logf("context7: enabled")
	}

	server := mcp.NewServer(tools, func(name string, args map[string]any) (string, error) {
		return app.handle(name, args)
	}, Version)

	fmt.Fprintf(os.Stderr, "search-mcp %s ready\n", Version)

	if err := server.Run(); err != nil {
		logf("fatal: %v", err)
		os.Exit(1)
	}
}

func (a *app) handle(name string, args map[string]any) (string, error) {
	switch name {
	case "web_search":
		return a.webSearch(args)
	case "fetch_page":
		return a.fetchPage(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// ─── web_search (dispatcher: single vs parallel) ────────────────

func (a *app) webSearch(args map[string]any) (string, error) {
	if rawQ, ok := args["queries"].([]any); ok && len(rawQ) > 0 {
		return a.parallelSearch(rawQ, args)
	}
	if _, ok := args["query"]; ok {
		return a.singleSearch(args)
	}
	return "", fmt.Errorf("provide 'query' or 'queries'")
}

// ─── single search + auto-fetch ─────────────────────────────────

func (a *app) singleSearch(args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	level := levelArg(args, "medium")
	site, _ := args["site"].(string)
	fetchCount := searchLevel[level]

	t := a.tc.NewThread("main")
	defer a.tc.CloseAll()
	defer a.closeThread(t)

	useCtx7, _ := args["context7"].(bool)

	var ctx7Result *context7.Result
	var ctx7Done chan struct{}
	if useCtx7 && a.ctx7 != nil {
		ctx7Done = make(chan struct{})
		go func() {
			defer close(ctx7Done)
			ctx7Result = a.ctx7.FullQuery(query)
		}()
	}

	results, err := a.doSearch(query, site, fetchCount, t)
	if err != nil {
		return "", err
	}

	pr := a.fetchPages(t, results, fetchCount, maxPageCharsSingle)

	if ctx7Done != nil {
		<-ctx7Done
	}

	summary := buildSummary(len(results), len(pr.pages), pr.skipped)

	out := map[string]any{
		"query":         query,
		"level":         level,
		"summary":       summary,
		"results":       results,
		"pages_fetched": len(pr.pages),
		"pages":         pr.pages,
		"skipped_urls":  pr.skippedURLs,
	}
	if ctx7Result != nil {
		out["context7"] = ctx7Result
	}
	return jsonString(out), nil
}

// buildSummary creates a human-readable summary string for web_search results.
func buildSummary(totalURLs, fetched, skipped int) string {
	s := fmt.Sprintf("%d results · %d fetched", totalURLs, fetched)
	if skipped > 0 {
		s += fmt.Sprintf(" (%d skipped)", skipped)
	}
	return s
}

// ─── parallel search + auto-fetch ───────────────────────────────

type querySpec struct {
	Query string
	Site  string
	Level string
	ID    string
}

func (a *app) parallelSearch(rawQueries []any, args map[string]any) (string, error) {
	if len(rawQueries) > 5 {
		return "", fmt.Errorf("max 5 parallel queries")
	}

	defer a.tc.CloseAll()
	defaultLevel := levelArg(args, "medium")

	specs := make([]querySpec, 0, len(rawQueries))
	for i, q := range rawQueries {
		switch v := q.(type) {
		case string:
			specs = append(specs, querySpec{
				Query: v, Level: defaultLevel,
				ID: fmt.Sprintf("t%d", i),
			})
		case map[string]any:
			specs = append(specs, querySpec{
				Query: orDefault(fmt.Sprint(v["query"]), ""),
				Site:  orDefault(fmt.Sprint(v["site"]), ""),
				Level: orDefault(fmt.Sprint(v["level"]), defaultLevel),
				ID:    orDefault(fmt.Sprint(v["id"]), fmt.Sprintf("t%d", i)),
			})
		}
	}

	a.log("parallel: %d threads (default level=%s)", len(specs), defaultLevel)

	type threadResult struct {
		ID           string              `json:"id"`
		Query        string              `json:"query"`
		Level        string              `json:"level"`
		Results      []google.Result     `json:"results"`
		PagesFetched int                 `json:"pages_fetched"`
		PagesSkipped int                 `json:"pages_skipped"`
		Pages        []map[string]any    `json:"pages"`
		SkippedURLs  []map[string]string `json:"skipped_urls,omitempty"`
		Context7     *context7.Result    `json:"context7,omitempty"`
		Error        string              `json:"error,omitempty"`
	}

	useCtx7, _ := args["context7"].(bool)

	var all []threadResult
	var allSkippedURLs []map[string]string

	for i, spec := range specs {
		if i > 0 {
			jitter := 2000 + rand.Intn(1000)
			time.Sleep(time.Duration(jitter) * time.Millisecond)
		}
		r := threadResult{ID: spec.ID, Query: spec.Query, Level: spec.Level}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					r.Error = fmt.Sprintf("panic: %v", rec)
					a.log("parallel: panic in thread %s: %v", spec.ID, rec)
				}
			}()

			fetchCount := searchLevel[spec.Level]
			if fetchCount == 0 {
				fetchCount = searchLevel["medium"]
			}

			t := a.tc.NewThread(spec.ID)
			defer a.closeThread(t)

			var ctx7Done chan struct{}
			if useCtx7 && a.ctx7 != nil {
				ctx7Done = make(chan struct{})
				go func() {
					defer close(ctx7Done)
					r.Context7 = a.ctx7.FullQuery(spec.Query)
				}()
			}

			results, err := a.doSearch(spec.Query, spec.Site, fetchCount, t)
			if err != nil {
				r.Error = err.Error()
				return
			}
			r.Results = results

			pages := a.fetchPages(t, results, fetchCount, maxPageCharsParallel)
			r.Pages = pages.pages
			r.PagesFetched = len(pages.pages)
			r.PagesSkipped = pages.skipped
			r.SkippedURLs = pages.skippedURLs

			if ctx7Done != nil {
				<-ctx7Done
			}
		}()

		all = append(all, r)
		allSkippedURLs = append(allSkippedURLs, r.SkippedURLs...)
	}

	success, totalPages, totalSkipped := 0, 0, 0
	for _, tr := range all {
		if tr.Error == "" {
			success++
		}
		totalPages += tr.PagesFetched
		totalSkipped += tr.PagesSkipped
	}
	a.log("parallel: %d/%d ok (%d pages)", success, len(all), totalPages)

	summary := fmt.Sprintf("%d queries · %d ok · %d pages", len(all), success, totalPages)
	if totalSkipped > 0 {
		summary += fmt.Sprintf(" (%d skipped)", totalSkipped)
	}

	return jsonString(map[string]any{
		"threads": all, "total": len(all),
		"success": success, "total_pages": totalPages,
		"summary":      summary,
		"skipped_urls": allSkippedURLs,
	}), nil
}

// ─── shared: live search with pagination ───────────────────────

func (a *app) doSearch(query, site string, num int, t *kimi.Thread) ([]google.Result, error) {
	a.log("search [%s]: %q (target: %d)", t.Name(), query, num)

	var allResults []google.Result
	seen := make(map[string]bool)
	start := 0

	for len(allResults) < num {
		searchURL := google.SearchURLWithStart(query, site, 10, start)

		if err := t.Navigate(searchURL); err != nil {
			if len(allResults) > 0 {
				break // got some results, stop paginating
			}
			return nil, fmt.Errorf("navigate: %w", err)
		}

		// Simulate human scrolling and dwell time on each Google page
		a.simulateHumanBehavior(t)

		html, err := t.GetHTML()
		if err != nil {
			if len(allResults) > 0 {
				break
			}
			return nil, fmt.Errorf("get HTML: %w", err)
		}

		results, err := google.ParseResults(html)
		if err != nil {
			if len(allResults) > 0 {
				break
			}
			return nil, fmt.Errorf("parse: %w", err)
		}

		// Deduplicate and add
		added := 0
		for _, r := range results {
			if seen[r.URL] {
				continue
			}
			seen[r.URL] = true
			allResults = append(allResults, r)
			added++
		}

		a.log("  page %d: %d results (%d new, total: %d)",
			start/10+1, len(results), added, len(allResults))

		// Stop if no new results or Google has no more pages
		if added == 0 || len(results) < 5 {
			break
		}

		start += 10
		// Random delay between pagination requests to avoid bot detection
		if len(allResults) < num {
			jitter := 2000 + rand.Intn(1000)
			time.Sleep(time.Duration(jitter) * time.Millisecond)
		}
	}

	if len(allResults) > num {
		allResults = allResults[:num]
	}

	a.log("→ %d results (%d pages)", len(allResults), start/10+1)
	return allResults, nil
}

// ─── shared: auto-fetch pages ───────────────────────────────────

// pageResult holds fetch output with quality stats.
type pageResult struct {
	pages       []map[string]any
	skipped     int                 // count of pages skipped (blocked, error, etc.)
	skippedURLs []map[string]string // [{url, reason}]
}

// fetchPages auto-fetches search results as markdown until fetchCount pages
// succeed. Results are processed in concurrent batches of maxConcurrent (each
// goroutine gets its own tab and closes it on completion); after each batch
// the successes are counted and fetching stops as soon as the target is met.
// This way broken pages are skipped without eagerly fetching a large fixed
// buffer of extra results that would just be discarded. If no pages can be
// fetched, returns an empty pageResult.
func (a *app) fetchPages(t *kimi.Thread, results []google.Result, fetchCount int, maxChars int) pageResult {
	if len(results) == 0 {
		return pageResult{}
	}

	a.log("auto-fetch: up to %d pages [%s] (parallel, max 3 concurrent, staged)", fetchCount, t.Name())

	const maxConcurrent = 3

	type fetchResult struct {
		page       map[string]any
		skipReason string
	}

	pages := make([]map[string]any, 0, fetchCount)
	var skipped int
	var skippedURLs []map[string]string

	// Walk results in batches, stopping once fetchCount pages succeed. At most
	// maxConcurrent-1 results beyond the last needed one are fetched (the tail
	// of the final batch).
	for next := 0; next < len(results) && len(pages) < fetchCount; {
		batch := maxConcurrent
		if remaining := len(results) - next; remaining < batch {
			batch = remaining
		}

		batchRes := make([]fetchResult, batch)
		var wg sync.WaitGroup
		for j := 0; j < batch; j++ {
			wg.Add(1)
			go func(slot, idx int) {
				defer wg.Done()
				ft := a.tc.NewThread(fmt.Sprintf("%s-fetch-%d", t.Name(), idx))
				defer a.closeThread(ft)

				page, skipReason := a.fetchOnePage(ft, results[idx].URL, maxChars)
				batchRes[slot] = fetchResult{page: page, skipReason: skipReason}
			}(j, next+j)
		}
		wg.Wait()

		for j := 0; j < batch && len(pages) < fetchCount; j++ {
			fr := batchRes[j]
			url := results[next+j].URL
			if fr.page == nil {
				skipped++
				skippedURLs = append(skippedURLs, map[string]string{"url": url, "reason": fr.skipReason})
				a.log("  skip: %s", truncLog(url, 60))
				continue
			}
			pages = append(pages, fr.page)
		}
		next += batch
	}

	return pageResult{pages: pages, skipped: skipped, skippedURLs: skippedURLs}
}

// fetchOnePage navigates to url, extracts the main content and returns a
// populated page map with an empty skip reason on success. It returns nil and a
// non-empty reason if the page could not be fetched, is an error page, or
// belongs to a blocked domain.
func (a *app) fetchOnePage(t *kimi.Thread, url string, maxChars int) (map[string]any, string) {
	// Blocked domains (video/streaming): skip fetch entirely
	if google.IsBlockedDomain(url) {
		a.log("  skip (blocked): %s", truncLog(url, 60))
		return nil, "blocked domain"
	}

	if err := t.Navigate(url); err != nil {
		a.log("  navigate failed: %s: %v", truncLog(url, 60), err)
		return nil, "navigate error"
	}
	html, err := t.GetHTML()
	if err != nil {
		a.log("  HTML failed: %s: %v", truncLog(url, 60), err)
		return nil, "HTML error"
	}
	content, err := parser.Extract(html, url)
	if err != nil {
		a.log("  parse failed: %s: %v", truncLog(url, 60), err)
		return nil, "parse error"
	}

	// Detect error pages (DNS, 404, Cloudflare challenge, etc.)
	if isErrorPage(content.Markdown) {
		a.log("  error page detected: %s", truncLog(url, 60))
		return nil, "error page detected"
	}

	truncatedMD, origLen, wasTruncated := middleTruncate(content.Markdown, maxChars)
	return map[string]any{
		"url":             url,
		"title":           content.Title,
		"markdown":        truncatedMD,
		"truncated":       wasTruncated,
		"original_length": origLen,
	}, ""
}

// ─── fetch_page (standalone) ────────────────────────────────────

func (a *app) fetchPage(args map[string]any) (string, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return "", fmt.Errorf("url is required")
	}

	t := a.tc.NewThread("main")
	defer a.tc.CloseAll()
	defer a.closeThread(t)

	a.log("page FETCH: %s", truncLog(url, 80))

	if err := t.Navigate(url); err != nil {
		return "", fmt.Errorf("navigate: %w", err)
	}
	html, err := t.GetHTML()
	if err != nil {
		return "", fmt.Errorf("get HTML: %w", err)
	}
	content, err := parser.Extract(html, url)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}

	// Reject error pages (DNS, 404, Cloudflare challenge, etc.) so that
	// direct fetch_page callers — typically an LLM — never consume them.
	if isErrorPage(content.Markdown) {
		a.log("error page detected: %s", truncLog(url, 60))
		return "", fmt.Errorf("error page detected at %s (likely 404, DNS, or challenge)", url)
	}

	truncatedMD, origLen, wasTruncated := middleTruncate(content.Markdown, maxPageCharsFetch)

	return jsonString(map[string]any{
		"url": url, "title": content.Title, "byline": content.Byline,
		"markdown":  truncatedMD,
		"truncated": wasTruncated, "original_length": origLen,
		"summary": "1 page fetched (live)",
	}), nil
}

// ─── helpers ────────────────────────────────────────────────────

// closeThread best-effort closes a research thread. Errors are logged but not
// propagated — cleanup is non-critical and the registry is purged either way.
func (a *app) closeThread(t *kimi.Thread) {
	if err := t.Close(); err != nil {
		a.log("close thread %q: %v", t.Name(), err)
	}
}

// simulateHumanBehavior performs random scrolls and dwell time on a loaded
// page to mimic human browsing patterns and avoid bot detection.
func (a *app) simulateHumanBehavior(t *kimi.Thread) {
	scrolls := 1 + rand.Intn(3) // 1-3 smooth scroll movements
	for i := 0; i < scrolls; i++ {
		distance := 100 + rand.Intn(600) // 100-700px
		_, _ = t.Evaluate(fmt.Sprintf(
			"window.scrollBy({top: %d, left: 0, behavior: 'smooth'});", distance))
		time.Sleep(time.Duration(150+rand.Intn(350)) * time.Millisecond) // 150-500ms between scrolls
	}
	dwellTime := 1000 + rand.Intn(1000) // 1-2s additional page dwell
	time.Sleep(time.Duration(dwellTime) * time.Millisecond)
}

func levelArg(args map[string]any, def string) string {
	raw, _ := args["level"].(string)
	if raw == "" {
		return def
	}
	if _, ok := searchLevel[raw]; ok {
		return raw
	}
	return def
}

func orDefault(val, def string) string {
	if val == "" || val == "<nil>" {
		return def
	}
	return val
}

func jsonString(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[search-mcp] jsonString marshal error: %v\n", err)
		return `{"error": "marshal failed"}`
	}
	return string(b)
}

// middleTruncate performs UTF-8 safe middle-truncation: keeps the first ~70%
// and last ~30% of runes, cutting the middle. Returns truncated content
// (no embedded message), original rune count, and whether truncation occurred.
func middleTruncate(s string, maxChars int) (string, int, bool) {
	runes := []rune(s)
	originalLen := len(runes)
	if originalLen <= maxChars {
		return s, originalLen, false
	}
	headLen := maxChars * 70 / 100
	tailLen := maxChars - headLen
	result := string(runes[:headLen]) + "\n\n... (content truncated) ...\n\n" + string(runes[originalLen-tailLen:])
	return result, originalLen, true
}

// truncLog truncates a string for log display (head-only, byte-level).
// Used for logging URLs without pulling in the full middleTruncate machinery.
func truncLog(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// strongErrorPatterns are substrings (case-insensitive) that almost never
// appear in legitimate article content — a match alone marks the page as a
// browser/network error. Matched against the start of the extracted markdown
// so a DNS error page or Cloudflare challenge does not pollute the LLM's
// context.
var strongErrorPatterns = []string{
	"dns_probe_finished",
	"nxdomain",
	"this site can't be reached",
	"siteye ulaşılamıyor",
	"http error 404",
	"http error 5",
	"sayfa bulunamıyor",
	"just a moment", // Cloudflare interstitial
	"checking your browser",
	"you have been blocked",
	"attention required",
	"err_ssl",
	"err_connection",
	"hata kodu",
	"connection refused",
	"connection timed out",
	"this webpage is not available",
}

// weakErrorPatterns are phrases that also occur in legitimate technical
// content (security docs, HTTP guides, API references). They only mark an
// error page when the page as a whole is very short — real error pages carry
// little body text, whereas an article about "rate limiting" or "HTTP 404"
// has plenty.
var weakErrorPatterns = []string{
	"404 not found",
	"page not found",
	"sayfa bulunamadı",
	"access denied",
	"forbidden",
	"captcha",
	"rate limit",
	"too many requests",
}

// errorPageShortLen is the rune threshold below which weak patterns are
// trusted. Genuine error pages produce far less markdown than real articles.
const errorPageShortLen = 600

// isErrorPage reports whether the given markdown looks like a browser
// error page (DNS, 404, Cloudflare challenge, etc.) rather than real content.
func isErrorPage(markdown string) bool {
	if markdown == "" {
		return false
	}
	lower := strings.ToLower(markdown)
	// Only scan the first ~2KB to avoid false positives from the body.
	head := lower
	if len(head) > 2048 {
		head = head[:2048]
	}
	for _, pat := range strongErrorPatterns {
		if strings.Contains(head, pat) {
			return true
		}
	}
	// Weak patterns are common words in legitimate content, so only treat them
	// as error markers when the whole page is very short.
	if len([]rune(markdown)) < errorPageShortLen {
		for _, pat := range weakErrorPatterns {
			if strings.Contains(head, pat) {
				return true
			}
		}
	}
	return false
}
