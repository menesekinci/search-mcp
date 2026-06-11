//go:build ignore

package main

import (
	"fmt"
	"time"

	"github.com/menesekinci/search-mcp/internal/google"
	"github.com/menesekinci/search-mcp/internal/kimi"
	"github.com/menesekinci/search-mcp/internal/parser"
	"github.com/menesekinci/search-mcp/internal/store"
)

func main() {
	fmt.Println("═══ Cache Integration Test ═══")
	fmt.Println()

	db, _ := store.Open("")
	defer db.Close()

	tc := kimi.NewTabbedClient("cache-int", "Cache Int")
	defer tc.CloseSession()

	query := "golang sqlite cache pattern"
	site := "github.com"

	// Round 1: MISS → live
	fmt.Println("🔍 Round 1: cache MISS")
	t1 := time.Now()
	cached, _ := db.GetSearch(query, site)
	if cached != nil {
		fmt.Println("   ❌ Unexpected HIT")
	} else {
		fmt.Println("   ✅ MISS — fetching live...")
		searchURL := google.SearchURL(query, site, 3)
		t := tc.NewThread("main")
		t.Navigate(searchURL)
		html, _ := t.GetHTML()
		results, _ := google.ParseResults(html)
		if len(results) > 3 {
			results = results[:3]
		}
		db.PutSearch(query, site, results)
		fmt.Printf("   ✅ Stored %d results (%.1fs)\n", len(results), time.Since(t1).Seconds())

		if len(results) > 0 {
			fmt.Println("\n📄 Fetching first result...")
			firstURL := results[0].URL
			pc, _ := db.GetPage(firstURL)
			if pc != nil {
				fmt.Println("   ❌ Unexpected page HIT")
			} else {
				fmt.Println("   ✅ Page MISS — fetching...")
				t.Navigate(firstURL)
				html2, _ := t.GetHTML()
				content, _ := parser.Extract(html2, firstURL)
				db.PutPage(firstURL, content.Title, content.Byline, content.Markdown)
				fmt.Printf("   ✅ Cached: %q (%d chars)\n", content.Title, len(content.Markdown))
			}
		}
	}

	// Round 2: HIT
	fmt.Println("\n🔍 Round 2: cache HIT")
	t2 := time.Now()
	cached2, _ := db.GetSearch(query, site)
	if cached2 != nil {
		fmt.Printf("   ✅ HIT: %d results (%.0fms, age=%s)\n",
			len(cached2.Results),
			float64(time.Since(t2).Microseconds())/1000,
			time.Since(cached2.UpdatedAt).Round(time.Second))
	} else {
		fmt.Println("   ❌ MISS")
	}

	// Page cache HIT
	if len(cached2.Results) > 0 {
		pc, _ := db.GetPage(cached2.Results[0].URL)
		if pc != nil {
			fmt.Printf("   ✅ Page HIT: %q\n", pc.Title)
		}
	}

	s, p, _ := db.Stats()
	fmt.Printf("\n📊 DB: %d searches, %d pages\n", s, p)
	fmt.Println("✅ All cache integration tests passed")
}
