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
	"github.com/menesekinci/search-mcp/internal/store"
)

// ─── Configuration ──────────────────────────────────────────────

const Version = "v0.5.3"

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
One call = search + fetch. Cached in local SQLite (URL-deduped, permanent).

Single:  {"query": "...", "level": "high", "site": "github.com"}
Parallel: {"queries": [{"query":"...","site":"..."}, "plain string"], "level": "medium"}
+Context7: {"query": "...", "context7": true} — also queries library docs

Levels: low(6) · medium(12) · high(24) · crazy(48). Default: medium.`,
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"query":        {Type: "string", Description: "Search terms. Supports site:, filetype:, -term, \"phrase\"."},
				"queries":      {Type: "array", Description: "Multiple queries for parallel research (2-5)."},
				"level":        {Type: "string", Description: "low | medium | high | crazy"},
				"site":         {Type: "string", Description: "Restrict to domain (e.g. github.com)."},
				"max_age_days": {Type: "number", Description: "0 = force live. Omit = use cache."},
				"context7":     {Type: "boolean", Description: "Also query Context7 for library docs alongside web search."},
			},
		},
	},
	{
		Name: "fetch_page",
		Description: `Fetch a specific URL, extract main content, return clean markdown.
Use for direct links. Not needed after web_search (it auto-fetches).

{"url": "https://example.com/article", "max_age_days": 0}`,
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"url":          {Type: "string", Description: "Page URL."},
				"max_age_days": {Type: "number", Description: "0 = force live. Omit = use cache."},
			},
			Required: []string{"url"},
		},
	},
	{
		Name:        "cache_stats",
		Description: "Show cache statistics (searches, pages, size, date range).",
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{},
		},
	},
}

// ─── Application ────────────────────────────────────────────────

type app struct {
	tc   *kimi.TabbedClient
	db   *store.DB
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

	db, err := store.Open("")
	must(err, "open db", logf)
	defer db.Close()

	sCount, pCount, _ := db.Stats()
	logf("cache: %d searches, %d pages", sCount, pCount)

	sp, pp, _ := db.PurgeExpired(7)
	if sp+pp > 0 {
		logf("purged: %d searches, %d pages (older than 7 days)", sp, pp)
	}

	app := &app{
		tc:  kimi.NewTabbedClient("search-mcp", "Search MCP"),
		db:  db,
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

func must(err error, ctx string, logf logFn) {
	if err != nil {
		logf("fatal %s: %v", ctx, err)
		os.Exit(1)
	}
}

func (a *app) handle(name string, args map[string]any) (string, error) {
	switch name {
	case "web_search":
		return a.webSearch(args)
	case "fetch_page":
		return a.fetchPage(args)
	case "cache_stats":
		return a.cacheStats(args)
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
	maxAgeDays := intArg(args, "max_age_days", -1, -1, 365)
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

	results, fromCache, err := a.doSearch(query, site, fetchCount, maxAgeDays, t)
	if err != nil {
		return "", err
	}

	pr := a.fetchPages(t, results, fetchCount, maxPageCharsSingle, maxAgeDays)

	if ctx7Done != nil {
		<-ctx7Done
	}

	summary := buildSummary(len(results), len(pr.pages), pr.cached, pr.skipped)

	out := map[string]any{
		"query":         query,
		"level":         level,
		"summary":       summary,
		"results":       results,
		"from_cache":    fromCache,
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
func buildSummary(totalURLs, fetched, cached, skipped int) string {
	s := fmt.Sprintf("%d results · %d fetched", totalURLs, fetched)
	if cached > 0 || skipped > 0 {
		var parts []string
		if cached > 0 {
			parts = append(parts, fmt.Sprintf("%d cached", cached))
		}
		if skipped > 0 {
			parts = append(parts, fmt.Sprintf("%d skipped", skipped))
		}
		s += " (" + strings.Join(parts, ", ") + ")"
	}
	return s
}

// ─── parallel search + auto-fetch ───────────────────────────────

type querySpec struct {
	Query      string
	Site       string
	Level      string
	ID         string
	MaxAgeDays int
}

func (a *app) parallelSearch(rawQueries []any, args map[string]any) (string, error) {
	if len(rawQueries) > 5 {
		return "", fmt.Errorf("max 5 parallel queries")
	}

	defer a.tc.CloseAll()
	defaultLevel := levelArg(args, "medium")
	globalMaxAge := intArg(args, "max_age_days", -1, -1, 365)

	specs := make([]querySpec, 0, len(rawQueries))
	for i, q := range rawQueries {
		switch v := q.(type) {
		case string:
			specs = append(specs, querySpec{
				Query: v, Level: defaultLevel,
				ID:         fmt.Sprintf("t%d", i),
				MaxAgeDays: globalMaxAge,
			})
		case map[string]any:
			specMaxAge := intArg(v, "max_age_days", -1, -1, 365)
			maxAge := globalMaxAge
			if specMaxAge != -1 {
				maxAge = specMaxAge
			}
			specs = append(specs, querySpec{
				Query:      orDefault(fmt.Sprint(v["query"]), ""),
				Site:       orDefault(fmt.Sprint(v["site"]), ""),
				Level:      orDefault(fmt.Sprint(v["level"]), defaultLevel),
				ID:         orDefault(fmt.Sprint(v["id"]), fmt.Sprintf("t%d", i)),
				MaxAgeDays: maxAge,
			})
		}
	}

	a.log("parallel: %d threads (default level=%s)", len(specs), defaultLevel)

	type threadResult struct {
		ID           string              `json:"id"`
		Query        string              `json:"query"`
		Level        string              `json:"level"`
		Results      []google.Result     `json:"results"`
		FromCache    bool                `json:"from_cache"`
		PagesFetched int                 `json:"pages_fetched"`
		PagesCached  int                 `json:"pages_cached"`
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

			results, fromCache, err := a.doSearch(spec.Query, spec.Site, fetchCount, spec.MaxAgeDays, t)
			if err != nil {
				r.Error = err.Error()
				return
			}
			r.Results = results
			r.FromCache = fromCache

			pages := a.fetchPages(t, results, fetchCount, maxPageCharsParallel, spec.MaxAgeDays)
			r.Pages = pages.pages
			r.PagesFetched = len(pages.pages)
			r.PagesCached = pages.cached
			r.PagesSkipped = pages.skipped
			r.SkippedURLs = pages.skippedURLs

			if ctx7Done != nil {
				<-ctx7Done
			}
		}()

		all = append(all, r)
		allSkippedURLs = append(allSkippedURLs, r.SkippedURLs...)
	}

	success, cached, totalPages, totalSkipped := 0, 0, 0, 0
	for _, tr := range all {
		if tr.Error == "" {
			success++
		}
		if tr.FromCache {
			cached++
		}
		totalPages += tr.PagesFetched
		totalSkipped += tr.PagesSkipped
	}
	a.log("parallel: %d/%d ok (%d cached, %d pages)", success, len(all), cached, totalPages)

	summary := fmt.Sprintf("%d queries · %d ok · %d pages", len(all), success, totalPages)
	if cached > 0 || totalSkipped > 0 {
		var parts []string
		if cached > 0 {
			parts = append(parts, fmt.Sprintf("%d cached", cached))
		}
		if totalSkipped > 0 {
			parts = append(parts, fmt.Sprintf("%d skipped", totalSkipped))
		}
		summary += " (" + strings.Join(parts, ", ") + ")"
	}

	return jsonString(map[string]any{
		"threads": all, "total": len(all),
		"success": success, "from_cache": cached, "total_pages": totalPages,
		"summary":      summary,
		"skipped_urls": allSkippedURLs,
	}), nil
}

// ─── shared: search (cache → live, with pagination) ────────────

func (a *app) doSearch(query, site string, num int, maxAgeDays int, t *kimi.Thread) ([]google.Result, bool, error) {
	if maxAgeDays != 0 {
		if cached, err := a.db.GetSearch(query, site, maxAgeDays); err == nil && cached != nil {
			if len(cached.Results) > num {
				cached.Results = cached.Results[:num]
			}
			a.log("cache HIT [%s]: %q (%d results)", t.Name(), query, len(cached.Results))
			return cached.Results, true, nil
		}
	}

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
			return nil, false, fmt.Errorf("navigate: %w", err)
		}

		// Simulate human scrolling and dwell time on each Google page
		a.simulateHumanBehavior(t)

		html, err := t.GetHTML()
		if err != nil {
			if len(allResults) > 0 {
				break
			}
			return nil, false, fmt.Errorf("get HTML: %w", err)
		}

		results, err := google.ParseResults(html)
		if err != nil {
			if len(allResults) > 0 {
				break
			}
			return nil, false, fmt.Errorf("parse: %w", err)
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

	a.db.PutSearch(query, site, allResults)
	a.log("→ %d results (%d pages)", len(allResults), start/10+1)
	return allResults, false, nil
}

// ─── shared: auto-fetch pages ───────────────────────────────────

// pageResult holds fetch output with quality stats.
type pageResult struct {
	pages       []map[string]any
	cached      int                 // count of pages served from cache
	skipped     int                 // count of pages skipped (blocked, error, etc.)
	skippedURLs []map[string]string // [{url, reason}]
}

// fetchPages auto-fetches the top N search results as markdown in parallel.
// Uses a channel-based semaphore (max 3 concurrent) to limit live fetches.
// Each parallel goroutine gets its own tab and closes it on completion.
// Broken pages are skipped and the next result is tried, up to fetchCount
// total successes. If no pages can be fetched, returns an empty pageResult.
func (a *app) fetchPages(t *kimi.Thread, results []google.Result, fetchCount int, maxChars int, maxAgeDays int) pageResult {
	if len(results) == 0 {
		return pageResult{}
	}

	a.log("auto-fetch: up to %d pages [%s] (parallel, max 3 concurrent)", fetchCount, t.Name())

	// Launch goroutines for fetchCount+10 results (buffer for skips)
	numToFetch := len(results)
	if numToFetch > fetchCount+10 {
		numToFetch = fetchCount + 10
	}

	const maxConcurrent = 3
	sem := make(chan struct{}, maxConcurrent)

	type fetchResult struct {
		index      int
		page       map[string]any
		skipReason string
		fromCache  bool
	}

	resultCh := make(chan fetchResult, numToFetch)
	var wg sync.WaitGroup

	for i := 0; i < numToFetch; i++ {
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			ft := a.tc.NewThread(fmt.Sprintf("%s-fetch-%d", t.Name(), idx))
			defer a.closeThread(ft)

			page, skipReason := a.fetchOnePage(ft, url, maxChars, maxAgeDays)
			resultCh <- fetchResult{
				index:      idx,
				page:       page,
				skipReason: skipReason,
				fromCache:  page != nil && page["from_cache"] == true,
			}
		}(i, results[i].URL)
	}

	go func() { wg.Wait(); close(resultCh) }()

	// Collect results into index-ordered slice
	fetched := make([]fetchResult, numToFetch)
	for fr := range resultCh {
		fetched[fr.index] = fr
	}

	// Build output, respecting fetchCount limit
	pages := make([]map[string]any, 0, fetchCount)
	var cached, skipped int
	var skippedURLs []map[string]string

	for i := 0; i < numToFetch && len(pages) < fetchCount; i++ {
		fr := fetched[i]
		url := results[i].URL
		if fr.page == nil {
			skipped++
			skippedURLs = append(skippedURLs, map[string]string{"url": url, "reason": fr.skipReason})
			a.log("  skip: %s", truncLog(url, 60))
			continue
		}
		if fr.fromCache {
			cached++
		}
		pages = append(pages, fr.page)
	}

	return pageResult{pages: pages, cached: cached, skipped: skipped, skippedURLs: skippedURLs}
}

// fetchOnePage returns a populated page map and an empty skip reason on success,
// or nil and a non-empty reason if the page could not be fetched, is an error
// page, or belongs to a blocked domain.
func (a *app) fetchOnePage(t *kimi.Thread, url string, maxChars int, maxAgeDays int) (map[string]any, string) {
	// Blocked domains (video/streaming): skip fetch entirely
	for _, domain := range google.BlockedDomains {
		if strings.Contains(url, domain) {
			a.log("  skip (blocked): %s", truncLog(url, 60))
			return nil, "blocked domain"
		}
	}

	page := map[string]any{"url": url}

	// Cache hit (TTL-aware)
	if cached, err := a.db.GetPage(url, maxAgeDays); err == nil && cached != nil {
		if isErrorPage(cached.Markdown) {
			// Cached entry is an error page — purge and refetch.
			a.log("  cache purge (error page): %s", truncLog(url, 60))
			_ = a.db.DeletePage(url)
		} else {
			page["title"] = cached.Title
			truncatedMD, origLen, wasTruncated := middleTruncate(cached.Markdown, maxChars)
			page["markdown"] = truncatedMD
			page["truncated"] = wasTruncated
			page["original_length"] = origLen
			page["from_cache"] = true
			return page, ""
		}
	}

	// Live fetch
	if err := t.Navigate(url); err != nil {
		a.log("  navigate failed: %s: %v", truncLog(url, 60), err)
		// Stale fallback: serve expired cache entry if live fetch fails
		if stale, sErr := a.db.GetPageStale(url); sErr == nil && stale != nil && !isErrorPage(stale.Markdown) {
			a.log("  stale fallback: %s", truncLog(url, 60))
			page["title"] = stale.Title
			truncatedMD, origLen, wasTruncated := middleTruncate(stale.Markdown, maxChars)
			page["markdown"] = truncatedMD
			page["truncated"] = wasTruncated
			page["original_length"] = origLen
			page["from_cache"] = true
			page["stale"] = true
			return page, ""
		}
		return nil, "navigate error"
	}
	html, err := t.GetHTML()
	if err != nil {
		a.log("  HTML failed: %s: %v", truncLog(url, 60), err)
		// Stale fallback
		if stale, sErr := a.db.GetPageStale(url); sErr == nil && stale != nil && !isErrorPage(stale.Markdown) {
			a.log("  stale fallback: %s", truncLog(url, 60))
			page["title"] = stale.Title
			truncatedMD, origLen, wasTruncated := middleTruncate(stale.Markdown, maxChars)
			page["markdown"] = truncatedMD
			page["truncated"] = wasTruncated
			page["original_length"] = origLen
			page["from_cache"] = true
			page["stale"] = true
			return page, ""
		}
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
		// Do NOT cache error pages — they would poison future lookups.
		return nil, "error page detected"
	}

	// Store full markdown, return truncated
	a.db.PutPage(url, content.Title, content.Byline, content.Markdown)

	page["title"] = content.Title
	truncatedMD, origLen, wasTruncated := middleTruncate(content.Markdown, maxChars)
	page["markdown"] = truncatedMD
	page["truncated"] = wasTruncated
	page["original_length"] = origLen
	page["from_cache"] = false
	return page, ""
}

// ─── fetch_page (standalone) ────────────────────────────────────

func (a *app) fetchPage(args map[string]any) (string, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return "", fmt.Errorf("url is required")
	}
	maxAgeDays := intArg(args, "max_age_days", -1, -1, 365)

	t := a.tc.NewThread("main")
	defer a.tc.CloseAll()
	defer a.closeThread(t)

	if maxAgeDays != 0 {
		if cached, err := a.db.GetPage(url, maxAgeDays); err == nil && cached != nil {
			if isErrorPage(cached.Markdown) {
				a.log("page purge (error page): %s", truncLog(url, 60))
				_ = a.db.DeletePage(url)
			} else {
				a.log("page HIT: %s", truncLog(url, 60))
				truncatedMD, origLen, wasTruncated := middleTruncate(cached.Markdown, maxPageCharsFetch)
				return jsonString(map[string]any{
					"url": url, "title": cached.Title, "byline": cached.Byline,
					"markdown": truncatedMD, "from_cache": true,
					"truncated": wasTruncated, "original_length": origLen,
					"cached_at": cached.UpdatedAt.Format(time.RFC3339),
					"summary":   "1 page fetched (cached)",
				}), nil
			}
		}
	}

	a.log("page FETCH: %s", truncLog(url, 80))

	if err := t.Navigate(url); err != nil {
		// Stale fallback: serve expired cache entry if live fetch fails
		if stale, sErr := a.db.GetPageStale(url); sErr == nil && stale != nil && !isErrorPage(stale.Markdown) {
			a.log("page STALE: %s", truncLog(url, 60))
			truncatedMD, origLen, wasTruncated := middleTruncate(stale.Markdown, maxPageCharsFetch)
			return jsonString(map[string]any{
				"url": url, "title": stale.Title, "byline": stale.Byline,
				"markdown": truncatedMD, "from_cache": true, "stale": true,
				"truncated": wasTruncated, "original_length": origLen,
				"cached_at": stale.UpdatedAt.Format(time.RFC3339),
				"summary":   "1 page fetched (stale cache)",
			}), nil
		}
		return "", fmt.Errorf("navigate: %w", err)
	}
	html, err := t.GetHTML()
	if err != nil {
		// Stale fallback
		if stale, sErr := a.db.GetPageStale(url); sErr == nil && stale != nil && !isErrorPage(stale.Markdown) {
			a.log("page STALE: %s", truncLog(url, 60))
			truncatedMD, origLen, wasTruncated := middleTruncate(stale.Markdown, maxPageCharsFetch)
			return jsonString(map[string]any{
				"url": url, "title": stale.Title, "byline": stale.Byline,
				"markdown": truncatedMD, "from_cache": true, "stale": true,
				"truncated": wasTruncated, "original_length": origLen,
				"cached_at": stale.UpdatedAt.Format(time.RFC3339),
				"summary":   "1 page fetched (stale cache)",
			}), nil
		}
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
	a.db.PutPage(url, content.Title, content.Byline, content.Markdown)

	return jsonString(map[string]any{
		"url": url, "title": content.Title, "byline": content.Byline,
		"markdown": truncatedMD, "from_cache": false,
		"truncated": wasTruncated, "original_length": origLen,
		"summary": "1 page fetched (live)",
	}), nil
}

// ─── cache_stats ────────────────────────────────────────────────

func (a *app) cacheStats(args map[string]any) (string, error) {
	stats, err := a.db.CacheStats()
	if err != nil {
		return "", fmt.Errorf("cache_stats: %w", err)
	}
	sCount, pCount, _ := a.db.Stats()
	return jsonString(map[string]any{
		"searches":         stats.Searches,
		"pages":            stats.Pages,
		"total_size_bytes": stats.TotalSizeBytes,
		"total_size_mb":    float64(stats.TotalSizeBytes) / (1024 * 1024),
		"oldest_search":    stats.OldestSearch,
		"newest_page":      stats.NewestPage,
		"summary":          fmt.Sprintf("%d searches, %d pages, %.1f MB", sCount, pCount, float64(stats.TotalSizeBytes)/(1024*1024)),
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

func intArg(args map[string]any, key string, def, min, max int) int {
	v, ok := args[key].(float64)
	if !ok {
		return def
	}
	n := int(v)
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
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

// errorPagePatterns are substrings (case-insensitive) that mark a fetched
// page as a browser/network error rather than real content. Matched against
// the extracted markdown so a DNS error page or Cloudflare challenge does
// not pollute the LLM's context.
var errorPagePatterns = []string{
	"dns_probe_finished",
	"nxdomain",
	"this site can't be reached",
	"siteye ulaşılamıyor",
	"404 not found",
	"http error 404",
	"http error 5",
	"sayfa bulunamıyor",
	"sayfa bulunamadı",
	"page not found",
	"access denied",
	"forbidden",
	"just a moment",   // Cloudflare interstitial
	"checking your browser",
	"captcha",
	"you have been blocked",
	"attention required",
	"rate limit",
	"too many requests",
	"err_ssl",
	"err_connection",
	"hata kodu",
	"connection refused",
	"connection timed out",
	"this webpage is not available",
}

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
	for _, pat := range errorPagePatterns {
		if strings.Contains(head, pat) {
			return true
		}
	}
	return false
}
