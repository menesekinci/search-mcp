package google

import (
	"strings"
	"testing"
)

func TestParseResults_Organic(t *testing.T) {
	html := `
<html>
<body>
<div id="search">
  <div id="rso">
    <div class="MjjYud">
      <div class="A6K0A">
        <div class="wHYlTd Ww4FFb tF2Cxc asEBEc">
          <div class="N54PNb BToiNc">
            <div class="kb0PBd A9Y9g jGGQ5e">
              <div class="yuRUbf">
                <span class="V9tjod">
                  <a class="zReHs" href="https://github.com/gocolly/colly">
                    <h3>gocolly/colly: Elegant Scraper Framework</h3>
                  </a>
                </span>
              </div>
            </div>
            <div class="kb0PBd A9Y9g">
              <div class="VwiC3b yXK7lf">
                Colly provides a clean interface to write any kind of crawler.
              </div>
            </div>
            <div class="kb0PBd">
              <cite>https://github.com › gocolly › colly</cite>
              <span class="VuuXrf">GitHub</span>
            </div>
          </div>
        </div>
      </div>
    </div>
    <!-- AD (should be skipped) -->
    <div class="MjjYud">
      <div class="A6K0A">
        <span class="n6AgNe"></span>
        <script>googleadservices.com</script>
      </div>
    </div>
    <!-- Another organic -->
    <div class="MjjYud">
      <div class="A6K0A">
        <div class="tF2Cxc">
          <h3>Go Web Scraping Guide</h3>
          <a class="zReHs" href="https://example.com/go-scraping"></a>
          <div class="VwiC3b">A comprehensive guide to web scraping with Go.</div>
          <cite>https://example.com › go-scraping</cite>
          <span class="VuuXrf">Example</span>
        </div>
      </div>
    </div>
  </div>
</div>
</body>
</html>`

	results, err := ParseResults(html)
	if err != nil {
		t.Fatalf("ParseResults failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 organic results, got %d", len(results))
	}

	// Result 1
	if results[0].Title != "gocolly/colly: Elegant Scraper Framework" {
		t.Errorf("title 1: got %q", results[0].Title)
	}
	if results[0].URL != "https://github.com/gocolly/colly" {
		t.Errorf("url 1: got %q", results[0].URL)
	}
	if !strings.Contains(results[0].Snippet, "Colly provides") {
		t.Errorf("snippet 1: got %q", results[0].Snippet)
	}
	if results[0].SiteName != "GitHub" {
		t.Errorf("site 1: got %q", results[0].SiteName)
	}

	// Result 2
	if results[1].Title != "Go Web Scraping Guide" {
		t.Errorf("title 2: got %q", results[1].Title)
	}
	if results[1].URL != "https://example.com/go-scraping" {
		t.Errorf("url 2: got %q", results[1].URL)
	}
}

func TestParseResults_AdFiltered(t *testing.T) {
	html := `
<div class="MjjYud">
  <div class="A6K0A">
    <span class="n6AgNe"></span>
    <script>googleadservices.com/...</script>
  </div>
</div>`

	results, _ := ParseResults(html)
	if len(results) != 0 {
		t.Errorf("ad should be filtered, got %d results", len(results))
	}
}

func TestSearchURL(t *testing.T) {
	u := SearchURL("golang web scraping", "", 5)
	if !strings.Contains(u, "golang+web+scraping") {
		t.Errorf("query encoding: %s", u)
	}

	u = SearchURL("tauri mcp", "github.com", 10)
	if !strings.Contains(u, "site:github.com") {
		t.Errorf("site operator: %s", u)
	}
}
