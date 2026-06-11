//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/menesekinci/search-mcp/internal/google"
	"github.com/menesekinci/search-mcp/internal/kimi"
	"github.com/menesekinci/search-mcp/internal/parser"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: go run test_live.go <search query>")
		os.Exit(1)
	}
	query := os.Args[1]

	// SINGLE client — one session, one tab, reused across all navigations
	kc := kimi.NewClient("live-test", "Live Test")
	defer kc.CloseSession()

	fmt.Printf("🔍 Search: %s\n", query)

	// --- STEP 1: Google search ---
	searchURL := google.SearchURL(query, "", 5)
	fmt.Printf("   [1] Navigate → Google (ilk sekme, group_title ayarlanır)\n")
	if _, err := kc.Navigate(searchURL); err != nil {
		fmt.Printf("❌ Navigate: %v\n", err)
		os.Exit(1)
	}

	html, err := kc.GetHTML()
	if err != nil {
		fmt.Printf("❌ HTML: %v\n", err)
		os.Exit(1)
	}

	results, err := google.ParseResults(html)
	if err != nil {
		fmt.Printf("❌ Parse: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("   📋 %d sonuç\n\n", len(results))
	for i, r := range results[:min(3, len(results))] {
		fmt.Printf("   %d. %s\n      %s\n\n", i+1, r.Title, r.URL)
	}

	// --- STEP 2: Fetch first result (SAME TAB, no new tab!) ---
	if len(results) > 0 {
		firstURL := results[0].URL
		displayURL := firstURL
		if len(displayURL) > 80 {
			displayURL = displayURL[:80]
		}
		fmt.Printf("📄 [2] Navigate → %s (AYNI SEKME)\n", displayURL)
		if _, err := kc.Navigate(firstURL); err != nil {
			fmt.Printf("❌ Fetch navigate: %v\n", err)
			os.Exit(1)
		}

		html2, err := kc.GetHTML()
		if err != nil {
			fmt.Printf("❌ Fetch HTML: %v\n", err)
			os.Exit(1)
		}

		content, err := parser.Extract(html2, firstURL)
		if err != nil {
			fmt.Printf("❌ Extract: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("   Title: %s\n", content.Title)
		fmt.Printf("   Markdown: %d chars\n", len(content.Markdown))
		fmt.Printf("   Preview: %s...\n\n", content.Markdown[:min(200, len(content.Markdown))])

		// --- STEP 3: Google again (STILL SAME TAB) ---
		searchURL2 := google.SearchURL("tauri mcp server", "", 3)
		fmt.Printf("🔍 [3] Navigate → Google again (HALA AYNI SEKME)\n")
		if _, err := kc.Navigate(searchURL2); err != nil {
			fmt.Printf("❌ Navigate2: %v\n", err)
			os.Exit(1)
		}

		html3, _ := kc.GetHTML()
		results2, _ := google.ParseResults(html3)
		fmt.Printf("   📋 %d sonuç\n", len(results2))

		out, _ := json.MarshalIndent(results2[:min(2, len(results2))], "", "  ")
		fmt.Println(string(out))
	}

	fmt.Println("\n✅ Test tamam — tüm navigasyonlar tek sekmede yapıldı")
}
