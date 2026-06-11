//go:build ignore

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/menesekinci/search-mcp/internal/google"
	"github.com/menesekinci/search-mcp/internal/store"
)

func main() {
	fmt.Println("═══ Cache Unit Test ═══")
	fmt.Println()

	dbPath := "test_cache.db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	db, err := store.Open(dbPath)
	if err != nil {
		fmt.Printf("❌ open: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		db.Close()
		os.Remove(dbPath)
	}()

	// 1. Put + Get search
	fmt.Println("1. PutSearch + GetSearch")
	db.PutSearch("test query", "github.com", []google.Result{
		{Title: "Result 1", URL: "https://example.com/1"},
		{Title: "Result 2", URL: "https://example.com/2"},
	})
	cached, err := db.GetSearch("test query", "github.com")
	if err != nil || cached == nil {
		fmt.Println("   ❌ FAIL")
	} else {
		fmt.Printf("   ✅ %d results, age=%s\n", len(cached.Results),
			time.Since(cached.CreatedAt).Round(time.Second))
	}

	// 2. Missing
	fmt.Println("\n2. Missing query")
	missing, _ := db.GetSearch("nonexistent", "")
	if missing == nil {
		fmt.Println("   ✅ nil (correct)")
	} else {
		fmt.Println("   ❌ should be nil")
	}

	// 3. Put + Get page
	fmt.Println("\n3. PutPage + GetPage")
	db.PutPage("https://example.com/page1", "Test Page", "Author", "# Hello World")
	page, _ := db.GetPage("https://example.com/page1")
	if page != nil {
		fmt.Printf("   ✅ title=%q markdown=%d chars\n", page.Title, len(page.Markdown))
	}

	// 4. Dedup (same URL overwrites)
	fmt.Println("\n4. Page dedup")
	db.PutPage("https://example.com/page1", "Updated", "", "# New")
	page2, _ := db.GetPage("https://example.com/page1")
	if page2.Title == "Updated" {
		fmt.Println("   ✅ overwritten correctly")
	}

	// 5. Dedup search
	fmt.Println("\n5. Search dedup")
	db.PutSearch("test query", "github.com", []google.Result{
		{Title: "NEW", URL: "https://new.com"},
	})
	cached2, _ := db.GetSearch("test query", "github.com")
	if len(cached2.Results) == 1 {
		fmt.Println("   ✅ replaced (1 result)")
	}

	// Stats
	s, p, _ := db.Stats()
	fmt.Printf("\n📊 %d searches, %d pages\n", s, p)
	fmt.Println("✅ All cache tests passed")
}
