package kimi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const BaseURL = "http://127.0.0.1:10086"

// Client talks to the Kimi WebBridge daemon at the session level.
// One session = one Chrome tab group. Use TabbedClient for multi-tab research.
type Client struct {
	http       *http.Client
	session    string
	groupTitle string
	groupSet   bool // whether group_title has been sent
}

// Request / Response types
type Request struct {
	Action  string         `json:"action"`
	Args    map[string]any `json:"args"`
	Session string         `json:"session"`
}

type Response struct {
	OK    bool `json:"ok"`
	Data  any  `json:"data,omitempty"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type NavigateResult struct {
	Success bool   `json:"success"`
	URL     string `json:"url"`
	TabID   int64  `json:"tabId"`
}

type EvalResult struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// NewClient creates a session-level client. groupTitle labels the Chrome tab group.
func NewClient(session string, groupTitle string) *Client {
	return &Client{
		http:       &http.Client{Timeout: 30 * time.Second},
		session:    session,
		groupTitle: groupTitle,
	}
}

func (c *Client) do(action string, args map[string]any, v any) error {
	body, err := json.Marshal(Request{
		Action:  action,
		Args:    args,
		Session: c.session,
	})
	if err != nil {
		return fmt.Errorf("kimi: marshal: %w", err)
	}

	req, err := http.NewRequest("POST", BaseURL+"/command", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("kimi: req: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("kimi: do: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("kimi: read: %w", err)
	}

	var r Response
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("kimi: unmarshal: %w", err)
	}

	if !r.OK {
		msg := "unknown"
		if r.Error != nil {
			msg = r.Error.Message
		}
		return fmt.Errorf("kimi: %s", msg)
	}

	if v != nil && r.Data != nil {
		data, err := json.Marshal(r.Data)
		if err != nil {
			return fmt.Errorf("kimi: marshal data: %w", err)
		}
		if err := json.Unmarshal(data, v); err != nil {
			return fmt.Errorf("kimi: unmarshal data: %w", err)
		}
	}
	return nil
}

// Navigate navigates the CURRENT tab. Does not open a new tab.
// For the first-ever navigation this implicitly creates the first tab.
func (c *Client) Navigate(url string) (*NavigateResult, error) {
	args := map[string]any{
		"url":    url,
		"newTab": false,
	}
	if !c.groupSet && c.groupTitle != "" {
		args["group_title"] = c.groupTitle
		c.groupSet = true
	}
	var r NavigateResult
	if err := c.do("navigate", args, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// OpenTab opens a NEW tab in the session and navigates to url.
// The new tab becomes the current tab.
func (c *Client) OpenTab(url string, groupTitle ...string) (*NavigateResult, error) {
	args := map[string]any{
		"url":    url,
		"newTab": true,
	}
	if !c.groupSet && c.groupTitle != "" {
		args["group_title"] = c.groupTitle
		c.groupSet = true
	} else if len(groupTitle) > 0 && groupTitle[0] != "" {
		args["group_title"] = groupTitle[0]
	}
	var r NavigateResult
	if err := c.do("navigate", args, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// SwitchTab switches to a tab that contains urlPattern in its URL.
// All subsequent Navigate/Evaluate calls operate on this tab.
func (c *Client) SwitchTab(urlPattern string) error {
	return c.do("find_tab", map[string]any{
		"url":    urlPattern,
		"active": true,
	}, nil)
}

// Evaluate runs JS in the current tab.
func (c *Client) Evaluate(code string) (any, error) {
	var r EvalResult
	if err := c.do("evaluate", map[string]any{"code": code}, &r); err != nil {
		return nil, err
	}
	return r.Value, nil
}

// GetHTML returns document.documentElement.outerHTML from the current tab.
func (c *Client) GetHTML() (string, error) {
	v, err := c.Evaluate("document.documentElement.outerHTML")
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("kimi: expected string HTML, got %T", v)
	}
	return s, nil
}

// GetTitle returns document.title from the current tab.
func (c *Client) GetTitle() (string, error) {
	v, err := c.Evaluate("document.title")
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("kimi: expected string title, got %T", v)
	}
	return s, nil
}

// CloseSession closes all tabs in the session.
func (c *Client) CloseSession() error {
	_ = c.do("close_session", nil, nil)
	return nil
}

// CloseTab closes the current tab in the session. The session itself
// (and any Chrome tab group) is preserved.
func (c *Client) CloseTab() error {
	return c.do("close_tab", nil, nil)
}
