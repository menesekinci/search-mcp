//go:build ignore

package main

import (
	"fmt"
	"time"

	"github.com/menesekinci/search-mcp/internal/google"
	"github.com/menesekinci/search-mcp/internal/kimi"
	"github.com/menesekinci/search-mcp/internal/store"
)

func main() {
	fmt.Println("═══ URL-Dedup Cache Test (no expiry) ═══")
	fmt.Println()

	db, _ := store.Open("")
	defer db.Close()

	tc := kimi.NewTabbedClient("dedup-test", "Dedup Test")
	defer tc.CloseSession()

	query := "golang chan select pattern"
	site := ""

	// Round 1: MISS → live → store
	fmt.Println("1️⃣  First search — cache MISS")
	cached, _ := db.GetSearch(query, site)
	if cached != nil {
		fmt.Println("   ❌ Unexpected HIT")
	} else {
		searchURL := google.SearchURL(query, site, 3)
		t := tc.NewThread("main")
		t.Navigate(searchURL)
		html, _ := t.GetHTML()
		results, _ := google.ParseResults(html)
		if len(results) > 3 {
			results = results[:3]
		}
		db.PutSearch(query, site, results)
		fmt.Printf("   ✅ Stored %d results\n", len(results))
		for i, r := range results {
			fmt.Printf("      %d. %s\n", i+1, r.URL[:min(60, len(r.URL))])
		}
	}

	// Round 2: HIT (no expiry!)
	fmt.Println("\n2️⃣  Same search — cache HIT (no expiry)")
	cached, _ = db.GetSearch(query, site)
	if cached != nil {
		fmt.Printf("   ✅ HIT: %d results, age=%s\n",
			len(cached.Results), time.Since(cached.UpdatedAt).Round(time.Second))
	} else {
		fmt.Println("   ❌ Unexpected MISS")
	}

	// Round 3: Force fresh
	fmt.Println("\n3️⃣  Force fresh (max_age_days=0) — cache MISS")
	cached, _ = db.GetSearch(query, site)
	if cached != nil {
		fmt.Println("   ✅ Cache exists, but forcing re-fetch...")
		searchURL := google.SearchURL(query, site, 3)
		t := tc.NewThread("main")
		t.Navigate(searchURL)
		html, _ := t.GetHTML()
		results, _ := google.ParseResults(html)
		if len(results) > 3 {
			results = results[:3]
		}
		db.PutSearch(query, site, results)
		fmt.Printf("   ✅ Re-fetched %d results (replaced old)\n", len(results))
	}

	// Page dedup test
	fmt.Println("\n4️⃣  Page dedup — same URL overwrites")
	url := "https://gobyexample.com/channels"
	db.PutPage(url, "Old Title", "", "# Old")
	page1, _ := db.GetPage(url)
	fmt.Printf("   First put: %q\n", page1.Title)

	db.PutPage(url, "Go by Example: Channels", "", "## Updated markdown content")
	page2, _ := db.GetPage(url)
	fmt.Printf("   Second put (same URL): %q ✅\n", page2.Title)

	s, p, _ := db.Stats()
	fmt.Printf("\n📊 DB: %d searches, %d pages (no duplicates)\n", s, p)
}
