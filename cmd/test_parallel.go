//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/menesekinci/search-mcp/internal/google"
	"github.com/menesekinci/search-mcp/internal/kimi"
)

func main() {
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║   search-mcp: PARALLEL RESEARCH TEST    ║")
	fmt.Println("║   3 thread, 3 tab, aynı Chrome grubu    ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Println()

	tc := kimi.NewTabbedClient("parallel-test", "Search MCP Test")
	defer tc.CloseSession()

	queries := []struct {
		id    string
		query string
		site  string
	}{
		{id: "github-mcp", query: "mcp server", site: "github.com"},
		{id: "tauri-docs", query: "tauri plugin", site: "docs.rs"},
		{id: "go-scraping", query: "web scraping", site: ""},
	}

	var wg sync.WaitGroup
	results := make([]string, len(queries))
	start := time.Now()

	for i, q := range queries {
		wg.Add(1)
		go func(idx int, spec struct {
			id    string
			query string
			site  string
		}) {
			defer wg.Done()

			searchURL := google.SearchURL(spec.query, spec.site, 3)
			displayURL := searchURL
			if len(displayURL) > 80 {
				displayURL = displayURL[:80]
			}
			t := tc.NewThread(spec.id)

			fmt.Printf("[%s] 🔍 %s\n", spec.id, displayURL)
			if err := t.Navigate(searchURL); err != nil {
				results[idx] = fmt.Sprintf("[%s] ❌ navigate: %v", spec.id, err)
				return
			}

			html, err := t.GetHTML()
			if err != nil {
				results[idx] = fmt.Sprintf("[%s] ❌ HTML: %v", spec.id, err)
				return
			}

			parsed, err := google.ParseResults(html)
			if err != nil {
				results[idx] = fmt.Sprintf("[%s] ❌ parse: %v", spec.id, err)
				return
			}

			out, _ := json.MarshalIndent(map[string]any{
				"thread":  spec.id,
				"query":   spec.query,
				"site":    spec.site,
				"count":   len(parsed),
				"results": parsed,
			}, "", "  ")
			results[idx] = string(out)
			fmt.Printf("[%s] ✅ %d sonuç\n", spec.id, len(parsed))
		}(i, q)
	}

	wg.Wait()
	elapsed := time.Since(start)

	fmt.Println()
	for _, r := range results {
		fmt.Println(r[:min(500, len(r))])
		fmt.Println("---")
	}
	fmt.Printf("\n⏱️  %d thread %v'de tamamlandı\n", len(queries), elapsed.Round(time.Millisecond))
}
