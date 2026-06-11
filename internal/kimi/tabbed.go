package kimi

import (
	"fmt"
	"sync"
)

// TabbedClient manages multiple tabs within a single Kimi session (Chrome group).
// Each research thread gets its own tab. All tabs share the same Chrome group.
//
// Thread safety: operations on different tabs are serialized via a session-level
// mutex because Kimi has a single "current tab" concept per session.
type TabbedClient struct {
	client *Client
	mu     sync.Mutex
	nextID int

	// tracks each thread's current URL for SwitchTab
	threads map[string]*threadState
}

type threadState struct {
	currentURL string // last known URL for this thread's tab
}

// NewTabbedClient wraps a session-level Client with multi-tab support.
func NewTabbedClient(session string, groupTitle string) *TabbedClient {
	return &TabbedClient{
		client:  NewClient(session, groupTitle),
		threads: make(map[string]*threadState),
	}
}

// Thread represents a single research thread with its own tab.
// Created by TabbedClient.NewThread or obtained via TabbedClient.Thread.
type Thread struct {
	tc   *TabbedClient
	name string
	tabOpened bool // has this thread's tab been opened yet?
}

// NewThread creates a new research thread. The tab is NOT opened yet —
// it opens on the first Navigate call.
func (tc *TabbedClient) NewThread(name string) *Thread {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if _, exists := tc.threads[name]; exists {
		// Return existing thread
		return &Thread{tc: tc, name: name, tabOpened: true}
	}

	tc.threads[name] = &threadState{}
	return &Thread{tc: tc, name: name}
}

// Navigate goes to url in this thread's tab. Opens a new tab on first call,
// reuses the existing tab on subsequent calls.
// Automatically switches to the correct tab before navigating.
func (t *Thread) Navigate(url string) error {
	t.tc.mu.Lock()
	defer t.tc.mu.Unlock()

	state, ok := t.tc.threads[t.name]
	if !ok {
		return fmt.Errorf("thread %q not found", t.name)
	}

	if t.tabOpened && state.currentURL != "" {
		// Switch back to this thread's tab before navigating
		_ = t.tc.client.SwitchTab(state.currentURL)
		// Navigate within the tab (newTab=false)
		if _, err := t.tc.client.Navigate(url); err != nil {
			return err
		}
	} else {
		// First time: open a new tab in the group
		if _, err := t.tc.client.OpenTab(url); err != nil {
			return err
		}
		t.tabOpened = true
	}

	state.currentURL = url
	return nil
}

// SwitchTo ensures this thread's tab is the active one.
func (t *Thread) SwitchTo() error {
	t.tc.mu.Lock()
	defer t.tc.mu.Unlock()

	state, ok := t.tc.threads[t.name]
	if !ok || state.currentURL == "" {
		return fmt.Errorf("thread %q has no active tab", t.name)
	}

	return t.tc.client.SwitchTab(state.currentURL)
}

// GetHTML returns the full page HTML from this thread's tab.
func (t *Thread) GetHTML() (string, error) {
	t.tc.mu.Lock()
	defer t.tc.mu.Unlock()

	state := t.tc.threads[t.name]
	if state.currentURL != "" {
		_ = t.tc.client.SwitchTab(state.currentURL)
	}
	return t.tc.client.GetHTML()
}

// Evaluate runs JS in this thread's tab.
func (t *Thread) Evaluate(code string) (any, error) {
	t.tc.mu.Lock()
	defer t.tc.mu.Unlock()

	state := t.tc.threads[t.name]
	if state.currentURL != "" {
		_ = t.tc.client.SwitchTab(state.currentURL)
	}
	return t.tc.client.Evaluate(code)
}

// Name returns the thread's identifier.
func (t *Thread) Name() string { return t.name }

// Close closes this thread's tab and removes it from the registry.
// After Close, the thread must not be used. Safe to call multiple times.
func (t *Thread) Close() error {
	t.tc.mu.Lock()
	defer t.tc.mu.Unlock()

	state, ok := t.tc.threads[t.name]
	if !ok {
		return nil
	}

	var closeErr error
	if state.currentURL != "" {
		_ = t.tc.client.SwitchTab(state.currentURL)
		closeErr = t.tc.client.CloseTab()
	}

	delete(t.tc.threads, t.name)
	return closeErr
}

// CloseSession closes all tabs in the session.
func (tc *TabbedClient) CloseSession() error {
	return tc.client.CloseSession()
}
