//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/menesekinci/search-mcp/internal/google"
	"github.com/menesekinci/search-mcp/internal/kimi"
	"github.com/menesekinci/search-mcp/internal/parser"
	"github.com/menesekinci/search-mcp/internal/store"
)

func main() {
	db, _ := store.Open("")
	defer db.Close()

	tc := kimi.NewTabbedClient("pipeline-test", "Pipeline Test")
	defer tc.CloseSession()

	level := "low"
	if len(os.Args) > 1 {
		level = os.Args[1]
	}
	query := "golang database sql patterns"
	if len(os.Args) > 2 {
		query = os.Args[2]
	}

	fmt.Printf("═══ Pipeline Test: %s (level=%s) ═══\n\n", level, query)

	start := time.Now()

	// Step 1: Search
	fmt.Println("🔍 Step 1: Google search...")
	t := tc.NewThread("main")
	searchURL := google.SearchURL(query, "", 10)
	t.Navigate(searchURL)
	html, _ := t.GetHTML()
	results, _ := google.ParseResults(html)

	fetchCount := map[string]int{"low": 6, "medium": 12, "high": 24, "crazy": 48}[level]
	if len(results) > fetchCount {
		results = results[:fetchCount]
	}
	fmt.Printf("   %d results\n\n", len(results))
	for i, r := range results {
		fmt.Printf("   %d. %s\n      %s\n", i+1, r.Title, r.URL[:min(70, len(r.URL))])
	}

	// Step 2: Auto-fetch pages
	fmt.Printf("\n📄 Step 2: Auto-fetching %d pages...\n", fetchCount)
	type page struct {
		URL      string `json:"url"`
		Title    string `json:"title"`
		Markdown string `json:"markdown"`
		Chars    int    `json:"chars"`
	}

	var pages []page
	toFetch := fetchCount
	if len(results) < toFetch {
		toFetch = len(results)
	}

	for i := 0; i < toFetch; i++ {
		url := results[i].URL
		fmt.Printf("   [%d/%d] %s\n", i+1, toFetch, url[:min(60, len(url))])

		// Check cache
		if cached, err := db.GetPage(url); err == nil && cached != nil {
			fmt.Printf("          ↳ cache HIT: %q (%d chars)\n", cached.Title, len(cached.Markdown))
			pages = append(pages, page{URL: url, Title: cached.Title, Markdown: cached.Markdown[:200], Chars: len(cached.Markdown)})
			db.PutPage(url, cached.Title, cached.Byline, cached.Markdown) // touch updated_at
			continue
		}

		if err := t.Navigate(url); err != nil {
			fmt.Printf("          ↳ FAIL: %v\n", err)
			continue
		}
		html, _ := t.GetHTML()
		content, _ := parser.Extract(html, url)
		md := content.Markdown
		db.PutPage(url, content.Title, content.Byline, md)

		fmt.Printf("          ↳ %q (%d chars)\n", content.Title, len(md))
		pages = append(pages, page{URL: url, Title: content.Title, Markdown: md[:min(200, len(md))], Chars: len(md)})
	}

	elapsed := time.Since(start)
	fmt.Printf("\n⏱️  Pipeline completed in %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("📊 %d searches, %d pages fetched\n", len(results), len(pages))

	out, _ := json.MarshalIndent(map[string]any{
		"level":   level,
		"results": results,
		"pages":   pages,
	}, "", "  ")
	fmt.Printf("\n--- JSON ---\n%s\n", out[:min(2000, len(out))])
}
