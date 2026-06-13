package context7

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const BaseURL = "https://context7.com/api/v2"

type Client struct {
	APIKey     string
	HTTPClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

type LibraryResult struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Snippets    int    `json:"codeSnippets"`
	Reputation  string `json:"sourceReputation"`
	Score       int    `json:"benchmarkScore"`
}

type searchLibsResponse struct {
	Results []LibraryResult `json:"results"`
}

type codeBlock struct {
	Code string `json:"code"`
}

type CodeSnippet struct {
	CodeTitle string      `json:"codeTitle"`
	CodeList  []codeBlock `json:"codeList"`
}

type InfoSnippet struct {
	Content string `json:"content"`
}

type contextResponse struct {
	CodeSnippets []CodeSnippet `json:"codeSnippets"`
	InfoSnippets []InfoSnippet `json:"infoSnippets"`
}

type Result struct {
	Library      *LibraryResult `json:"library,omitempty"`
	Query        string         `json:"query"`
	CodeSnippets []CodeSnippet  `json:"codeSnippets,omitempty"`
	InfoSnippets []InfoSnippet  `json:"infoSnippets,omitempty"`
	Error        string         `json:"error,omitempty"`
}

func (c *Client) FullQuery(query string) *Result {
	libs, err := c.searchLibs(query)
	if err != nil {
		return &Result{Query: query, Error: err.Error()}
	}
	if len(libs.Results) == 0 {
		return &Result{Query: query}
	}

	best := libs.Results[0]

	ctx, err := c.getContext(best.ID, query)
	if err != nil {
		return &Result{Query: query, Library: &best, Error: err.Error()}
	}

	return &Result{
		Library:      &best,
		Query:        query,
		CodeSnippets: ctx.CodeSnippets,
		InfoSnippets: ctx.InfoSnippets,
	}
}

func (c *Client) searchLibs(query string) (*searchLibsResponse, error) {
	u, _ := url.Parse(BaseURL + "/libs/search")
	q := u.Query()
	q.Set("query", query)
	u.RawQuery = q.Encode()

	req, _ := http.NewRequest("GET", u.String(), nil)
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("context7 search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("context7 search: HTTP %d", resp.StatusCode)
	}

	var result searchLibsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("context7 search decode: %w", err)
	}
	return &result, nil
}

func (c *Client) getContext(libraryID, query string) (*contextResponse, error) {
	u, _ := url.Parse(BaseURL + "/context")
	q := u.Query()
	q.Set("libraryId", libraryID)
	q.Set("query", query)
	q.Set("type", "json")
	u.RawQuery = q.Encode()

	req, _ := http.NewRequest("GET", u.String(), nil)
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("context7 context: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("context7 context: HTTP %d", resp.StatusCode)
	}

	var result contextResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("context7 context decode: %w", err)
	}
	return &result, nil
}
