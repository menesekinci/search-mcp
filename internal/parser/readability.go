package parser

import (
	"fmt"

	readability "github.com/mackee/go-readability"
	md "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
)

// PageContent holds the extracted and converted page content.
type PageContent struct {
	Title    string `json:"title"`
	Byline   string `json:"byline,omitempty"`
	Markdown string `json:"markdown"`
	URL      string `json:"url,omitempty"`
}

// Extract cleans raw HTML (removes nav, ads, sidebar) and converts to markdown.
func Extract(html string, pageURL string) (*PageContent, error) {
	// Step 1: Extract main content using go-readability (Mozilla Readability port)
	article, err := readability.Extract(html, readability.DefaultOptions())
	if err != nil {
		return nil, fmt.Errorf("parser: readability extract: %w", err)
	}

	// Step 2: Get cleaned HTML of the main content
	var cleanedHTML string
	if article.Root != nil {
		cleanedHTML = readability.ToHTML(article.Root)
	} else {
		cleanedHTML = html
	}

	// Step 3: Convert to markdown using the simple API
	var opts []converter.ConvertOptionFunc
	if pageURL != "" {
		opts = append(opts, converter.WithDomain(pageURL))
	}

	markdown, err := md.ConvertString(cleanedHTML, opts...)
	if err != nil {
		return nil, fmt.Errorf("parser: markdown conversion: %w", err)
	}

	return &PageContent{
		Title:    article.Title,
		Byline:   article.Byline,
		Markdown: markdown,
		URL:      pageURL,
	}, nil
}
