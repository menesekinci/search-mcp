package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/menesekinci/search-mcp/internal/google"
	"github.com/menesekinci/search-mcp/internal/kimi"
	"github.com/menesekinci/search-mcp/internal/mcp"
	"github.com/menesekinci/search-mcp/internal/parser"
	"github.com/menesekinci/search-mcp/internal/setup"
	"github.com/menesekinci/search-mcp/internal/store"
)

// ─── Configuration ──────────────────────────────────────────────

var searchLevel = map[string]int{
	"low": 3, "medium": 6, "high": 12, "crazy": 24,
}

const (
	maxPageCharsSingle   = 15000 // per-page markdown in web_search
	maxPageCharsParallel = 10000 // per-page markdown in parallel mode
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

Levels: low(3) · medium(6) · high(12) · crazy(24). Default: medium.`,
		InputSchema: mcp.JSONSchema{
			Type: "object",
			Properties: map[string]mcp.Property{
				"query":        {Type: "string", Description: "Search terms. Supports site:, filetype:, -term, \"phrase\"."},
				"queries":      {Type: "array", Description: "Multiple queries for parallel research (2-5)."},
				"level":        {Type: "string", Description: "low | medium | high | crazy"},
				"site":         {Type: "string", Description: "Restrict to domain (e.g. github.com)."},
				"max_age_days": {Type: "number", Description: "0 = force live. Omit = use cache."},
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
}

// ─── Application ────────────────────────────────────────────────

type app struct {
	tc  *kimi.TabbedClient
	db  *store.DB
	log logFn
}

type logFn func(format string, v ...any)

func main() {
	// Setup wizard mode
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		setup.Run()
		return
	}

	// Version flag
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("search-mcp v0.5.1")
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

	app := &app{
		tc:  kimi.NewTabbedClient("search-mcp", "Search MCP"),
		db:  db,
		log: logf,
	}

	server := mcp.NewServer(tools, func(name string, args map[string]any) (string, error) {
		return app.handle(name, args)
	})

	fmt.Fprintf(os.Stderr, "search-mcp v0.5.1 ready\n")

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
	forceFresh := intArg(args, "max_age_days", -1, -1, 365) == 0
	fetchCount := searchLevel[level]

	results, fromCache, err := a.doSearch(query, site, fetchCount, forceFresh, "main")
	if err != nil {
		return "", err
	}

	pages := a.fetchPages(results, fetchCount, maxPageCharsSingle, "main")

	return jsonString(map[string]any{
		"query":         query,
		"level":         level,
		"results":       results,
		"from_cache":    fromCache,
		"pages_fetched": len(pages),
		"pages":         pages,
	}), nil
}

// ─── parallel search + auto-fetch ───────────────────────────────

type querySpec struct {
	Query      string
	Site       string
	Level      string
	ID         string
	ForceFresh bool
}

func (a *app) parallelSearch(rawQueries []any, args map[string]any) (string, error) {
	if len(rawQueries) > 5 {
		return "", fmt.Errorf("max 5 parallel queries")
	}

	defaultLevel := levelArg(args, "medium")
	globalForce := intArg(args, "max_age_days", -1, -1, 365) == 0

	specs := make([]querySpec, 0, len(rawQueries))
	for i, q := range rawQueries {
		switch v := q.(type) {
		case string:
			specs = append(specs, querySpec{
				Query: v, Level: defaultLevel,
				ID: fmt.Sprintf("t%d", i), ForceFresh: globalForce,
			})
		case map[string]any:
			specs = append(specs, querySpec{
				Query: fmt.Sprint(v["query"]), Site: fmt.Sprint(v["site"]),
				Level: orDefault(fmt.Sprint(v["level"]), defaultLevel),
				ID:    orDefault(fmt.Sprint(v["id"]), fmt.Sprintf("t%d", i)),
				ForceFresh: globalForce || intArg(v, "max_age_days", -1, -1, 365) == 0,
			})
		}
	}

	a.log("parallel: %d threads (default level=%s)", len(specs), defaultLevel)

	type threadResult struct {
		ID           string           `json:"id"`
		Query        string           `json:"query"`
		Level        string           `json:"level"`
		Results      []google.Result  `json:"results"`
		FromCache    bool             `json:"from_cache"`
		PagesFetched int              `json:"pages_fetched"`
		Pages        []map[string]any `json:"pages"`
		Error        string           `json:"error,omitempty"`
	}

	ch := make(chan threadResult, len(specs))

	for _, spec := range specs {
		go func(s querySpec) {
			r := threadResult{ID: s.ID, Query: s.Query, Level: s.Level}
			fetchCount := searchLevel[s.Level]
			if fetchCount == 0 {
				fetchCount = searchLevel["medium"]
			}

			results, fromCache, err := a.doSearch(s.Query, s.Site, fetchCount, s.ForceFresh, s.ID)
			if err != nil {
				r.Error = err.Error()
				ch <- r
				return
			}
			r.Results = results
			r.FromCache = fromCache

			pages := a.fetchPages(results, fetchCount, maxPageCharsParallel, s.ID)
			r.Pages = pages
			r.PagesFetched = len(pages)
			ch <- r
		}(spec)
	}

	var all []threadResult
	for range specs {
		all = append(all, <-ch)
	}

	success, cached, totalPages := 0, 0, 0
	for _, tr := range all {
		if tr.Error == "" {
			success++
		}
		if tr.FromCache {
			cached++
		}
		totalPages += tr.PagesFetched
	}
	a.log("parallel: %d/%d ok (%d cached, %d pages)", success, len(all), cached, totalPages)

	return jsonString(map[string]any{
		"threads": all, "total": len(all),
		"success": success, "from_cache": cached, "total_pages": totalPages,
	}), nil
}

// ─── shared: search (cache → live, with pagination) ────────────

func (a *app) doSearch(query, site string, num int, forceFresh bool, threadName string) ([]google.Result, bool, error) {
	if !forceFresh {
		if cached, err := a.db.GetSearch(query, site); err == nil && cached != nil {
			if len(cached.Results) > num {
				cached.Results = cached.Results[:num]
			}
			a.log("cache HIT [%s]: %q (%d results)", threadName, query, len(cached.Results))
			return cached.Results, true, nil
		}
	}

	a.log("search [%s]: %q (target: %d)", threadName, query, num)

	t := a.tc.NewThread(threadName)
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
	}

	if len(allResults) > num {
		allResults = allResults[:num]
	}

	a.db.PutSearch(query, site, allResults)
	a.log("→ %d results (%d pages)", len(allResults), start/10+1)
	return allResults, false, nil
}

// ─── shared: auto-fetch pages ───────────────────────────────────

func (a *app) fetchPages(results []google.Result, fetchCount int, maxChars int, threadName string) []map[string]any {
	toFetch := fetchCount
	if len(results) < toFetch {
		toFetch = len(results)
	}
	if toFetch == 0 {
		return nil
	}

	a.log("auto-fetch: %d pages [%s]", toFetch, threadName)
	t := a.tc.NewThread(threadName)
	pages := make([]map[string]any, 0, toFetch)

	for i := 0; i < toFetch; i++ {
		url := results[i].URL
		page := map[string]any{"url": url}

		// Cache hit
		if cached, err := a.db.GetPage(url); err == nil && cached != nil {
			page["title"] = cached.Title
			page["markdown"] = truncateStr(cached.Markdown, maxChars)
			page["from_cache"] = true
			pages = append(pages, page)
			continue
		}

		// Live fetch
		if err := t.Navigate(url); err != nil {
			page["title"] = "(navigate failed)"
			pages = append(pages, page)
			continue
		}
		html, err := t.GetHTML()
		if err != nil {
			page["title"] = "(HTML failed)"
			pages = append(pages, page)
			continue
		}
		content, err := parser.Extract(html, url)
		if err != nil {
			page["title"] = "(parse failed)"
			pages = append(pages, page)
			continue
		}

		// Store full markdown, return truncated
		a.db.PutPage(url, content.Title, content.Byline, content.Markdown)

		page["title"] = content.Title
		page["markdown"] = truncateStr(content.Markdown, maxChars)
		page["from_cache"] = false
		pages = append(pages, page)
	}
	return pages
}

// ─── fetch_page (standalone) ────────────────────────────────────

func (a *app) fetchPage(args map[string]any) (string, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return "", fmt.Errorf("url is required")
	}
	forceFresh := intArg(args, "max_age_days", -1, -1, 365) == 0

	if !forceFresh {
		if cached, err := a.db.GetPage(url); err == nil && cached != nil {
			a.log("page HIT: %s", truncateStr(url, 60))
			return jsonString(map[string]any{
				"url": url, "title": cached.Title, "byline": cached.Byline,
				"markdown": cached.Markdown, "from_cache": true,
				"cached_at": cached.UpdatedAt.Format(time.RFC3339),
			}), nil
		}
	}

	a.log("page FETCH: %s", truncateStr(url, 80))

	t := a.tc.NewThread("main")
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

	markdown := truncateStr(content.Markdown, maxPageCharsFetch)
	a.db.PutPage(url, content.Title, content.Byline, content.Markdown)

	return jsonString(map[string]any{
		"url": url, "title": content.Title, "byline": content.Byline,
		"markdown": markdown, "from_cache": false,
	}), nil
}

// ─── helpers ────────────────────────────────────────────────────

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
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n] + "\n\n... (truncated)"
	}
	return s
}
