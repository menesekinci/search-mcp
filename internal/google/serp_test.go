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

	_, err := ParseResults(html)
	if err == nil {
		t.Error("expected error for HTML with no organic results")
	}
}

func TestParseResults_LegacyGoogle(t *testing.T) {
	// Legacy Google SERP (circa 2010-2022): div.g containers with div.rc organic markers
	html := `
<html>
<body>
<div id="search">
  <div id="rso">
    <div class="g">
      <div class="rc">
        <h3 class="r"><a href="https://example.com/legacy1">Legacy Result One</a></h3>
        <div class="s"><span class="st">First legacy result snippet text.</span></div>
        <cite>https://example.com › legacy1</cite>
      </div>
    </div>
    <div class="g">
      <div class="rc">
        <h3 class="r"><a href="https://example.com/legacy2">Legacy Result Two</a></h3>
        <div class="s"><span class="st">Second legacy result snippet.</span></div>
        <cite>https://example.com › legacy2</cite>
      </div>
    </div>
    <!-- AD in legacy format: no div.rc inside -->
    <div class="g">
      <div class="ads">
        <span>Sponsored</span>
      </div>
    </div>
  </div>
</div>
</body>
</html>`

	results, err := ParseResults(html)
	if err != nil {
		t.Fatalf("ParseResults failed on legacy HTML: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 legacy results, got %d", len(results))
	}

	if results[0].Title != "Legacy Result One" {
		t.Errorf("title 1: got %q", results[0].Title)
	}
	if results[0].URL != "https://example.com/legacy1" {
		t.Errorf("url 1: got %q", results[0].URL)
	}

	if results[1].Title != "Legacy Result Two" {
		t.Errorf("title 2: got %q", results[1].Title)
	}
	if results[1].URL != "https://example.com/legacy2" {
		t.Errorf("url 2: got %q", results[1].URL)
	}
}

func TestParseResults_SemanticAttributeFallback(t *testing.T) {
	// Future/experimental Google: div[data-sokoban-container] with h3 as organic check
	html := `
<html>
<body>
<div id="search">
  <div data-sokoban-container="1">
    <h3><a href="https://example.com/semantic1">Semantic Result Alpha</a></h3>
    <div class="VwiC3b">Alpha snippet text here.</div>
    <cite>https://example.com › semantic1</cite>
  </div>
  <div data-sokoban-container="2">
    <h3><a href="https://example.com/semantic2">Semantic Result Beta</a></h3>
    <div class="VwiC3b">Beta snippet text here.</div>
    <cite>https://example.com › semantic2</cite>
  </div>
  <!-- Widget container (no h3 → filtered out) -->
  <div data-sokoban-container="3">
    <div class="widget">Knowledge panel</div>
  </div>
</div>
</body>
</html>`

	results, err := ParseResults(html)
	if err != nil {
		t.Fatalf("ParseResults failed on semantic attribute HTML: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 semantic results, got %d", len(results))
	}

	if results[0].Title != "Semantic Result Alpha" {
		t.Errorf("title 1: got %q", results[0].Title)
	}
	if results[0].URL != "https://example.com/semantic1" {
		t.Errorf("url 1: got %q", results[0].URL)
	}

	if results[1].Title != "Semantic Result Beta" {
		t.Errorf("title 2: got %q", results[1].Title)
	}
	if results[1].URL != "https://example.com/semantic2" {
		t.Errorf("url 2: got %q", results[1].URL)
	}
}

func TestParseResults_UltimateFallback(t *testing.T) {
	// Completely unknown HTML — should fall back to collecting all external links
	html := `
<html>
<body>
<div id="some-wrapper">
  <p>Some unstructured content</p>
  <a href="https://example.com/page1">Example Page One</a>
  <a href="https://google.com/search?q=test">Google Search Link (filtered)</a>
  <a href="https://example.com/page2">Example Page Two</a>
  <a href="#fragment">Fragment Link (filtered)</a>
  <a href="https://example.com/page3">Example Page Three</a>
  <a href="https://google.com/preferences">Google Preferences (filtered)</a>
  <!-- duplicate URL should be deduplicated -->
  <a href="https://example.com/page1">Example Page One Dup</a>
</div>
</body>
</html>`

	results, err := ParseResults(html)
	if err != nil {
		t.Fatalf("ParseResults failed on ultimate fallback HTML: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results from ultimate fallback, got %d", len(results))
	}

	if results[0].Title != "Example Page One" {
		t.Errorf("title 1: got %q", results[0].Title)
	}
	if results[0].URL != "https://example.com/page1" {
		t.Errorf("url 1: got %q", results[0].URL)
	}

	if results[1].Title != "Example Page Two" {
		t.Errorf("title 2: got %q", results[1].Title)
	}
	if results[1].URL != "https://example.com/page2" {
		t.Errorf("url 2: got %q", results[1].URL)
	}

	if results[2].Title != "Example Page Three" {
		t.Errorf("title 3: got %q", results[2].Title)
	}
	if results[2].URL != "https://example.com/page3" {
		t.Errorf("url 3: got %q", results[2].URL)
	}
}

func TestParseResults_EmptyHTML(t *testing.T) {
	_, err := ParseResults("")
	if err == nil {
		t.Error("expected error for empty HTML")
	}

	_, err = ParseResults("<html><body><p>No links here</p></body></html>")
	if err == nil {
		t.Error("expected error for HTML with no results")
	}
}

func TestParseResults_URLFallbackAttributes(t *testing.T) {
	// Test that URL extraction falls back through a[jsname] and a[data-ved]
	html := `
<html>
<body>
<div class="MjjYud">
  <div class="tF2Cxc">
    <h3>JSName Link</h3>
    <a jsname="UWckNb" href="https://example.com/jsname-result">JSName Link Text</a>
    <div class="VwiC3b">Snippet for jsname test.</div>
  </div>
</div>
<div class="MjjYud">
  <div class="tF2Cxc">
    <h3>data-ved Link</h3>
    <a data-ved="0ahUKEwj..." href="https://example.com/dataved-result">data-ved Link Text</a>
    <div class="VwiC3b">Snippet for data-ved test.</div>
  </div>
</div>
</body>
</html>`

	results, err := ParseResults(html)
	if err != nil {
		t.Fatalf("ParseResults failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].URL != "https://example.com/jsname-result" {
		t.Errorf("url 1 (jsname): got %q", results[0].URL)
	}
	if results[1].URL != "https://example.com/dataved-result" {
		t.Errorf("url 2 (data-ved): got %q", results[1].URL)
	}
}

func TestParseResults_Canary(t *testing.T) {
	// Canary / smoke test: parse a known fixture in all three selector formats
	// and verify at least 5 total results across the test suite.

	// This test verifies that the test suite itself is healthy.
	// For live Google canary testing, see cmd/test_live.go or run:
	//   go run cmd/test_live.go "golang tutorial"

	results, err := ParseResults(canaryFixture())
	if err != nil {
		t.Fatalf("canary fixture failed: %v", err)
	}
	if len(results) < 5 {
		t.Errorf("canary: expected at least 5 results, got %d", len(results))
	}
}

// canaryFixture returns a full SERP-like fixture with the primary MjjYud selector.
func canaryFixture() string {
	return `
<html>
<body>
<div id="search"><div id="rso">
  <div class="MjjYud"><div class="tF2Cxc"><h3><a class="zReHs" href="https://go.dev/">The Go Programming Language</a></h3><div class="VwiC3b">Go is an open source programming language that makes it simple to build secure, scalable systems.</div><cite>https://go.dev</cite><span class="VuuXrf">Go</span></div></div>
  <div class="MjjYud"><div class="tF2Cxc"><h3><a class="zReHs" href="https://pkg.go.dev/">Go Packages</a></h3><div class="VwiC3b">Go is an open source programming language that makes it easy to build simple, reliable, and efficient software.</div><cite>https://pkg.go.dev</cite><span class="VuuXrf">pkg.go.dev</span></div></div>
  <div class="MjjYud"><div class="tF2Cxc"><h3><a class="zReHs" href="https://github.com/golang/go">golang/go: The Go Programming Language</a></h3><div class="VwiC3b">The Go Programming Language. Contribute to golang/go development by creating an account on GitHub.</div><cite>https://github.com › golang › go</cite><span class="VuuXrf">GitHub</span></div></div>
  <div class="MjjYud"><div class="tF2Cxc"><h3><a class="zReHs" href="https://en.wikipedia.org/wiki/Go_(programming_language)">Go (programming language) - Wikipedia</a></h3><div class="VwiC3b">Go is a statically typed, compiled high-level general purpose programming language.</div><cite>https://en.wikipedia.org › wiki › Go_(programming_language)</cite><span class="VuuXrf">Wikipedia</span></div></div>
  <div class="MjjYud"><div class="tF2Cxc"><h3><a class="zReHs" href="https://go.dev/doc/">Documentation - The Go Programming Language</a></h3><div class="VwiC3b">The Go programming language is an open source project to make programmers more productive.</div><cite>https://go.dev › doc</cite><span class="VuuXrf">go.dev</span></div></div>
  <!-- AD (should be skipped) -->
  <div class="MjjYud"><div class="A6K0A"><span class="n6AgNe"></span></div></div>
</div></div>
</body>
</html>`
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
