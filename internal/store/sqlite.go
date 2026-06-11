package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/menesekinci/search-mcp/internal/google"
)

type DB struct{ db *sql.DB }

func Open(path string) (*DB, error) {
	if path == "" {
		home, _ := os.UserHomeDir()
		dir := filepath.Join(home, ".search-mcp")
		os.MkdirAll(dir, 0700)
		path = filepath.Join(dir, "cache.db")
	}
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	d := &DB{db: db}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) migrate() error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS searches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			query TEXT NOT NULL,
			site TEXT NOT NULL DEFAULT '',
			results_json TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
			UNIQUE(query, site)
		);
		CREATE TABLE IF NOT EXISTS pages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			url TEXT NOT NULL UNIQUE,
			title TEXT NOT NULL DEFAULT '',
			byline TEXT NOT NULL DEFAULT '',
			markdown TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_searches_updated ON searches(updated_at);
		CREATE INDEX IF NOT EXISTS idx_pages_url ON pages(url);
	`)
	return err
}

// ─── Search cache ───────────────────────────────────────────────

type CachedSearch struct {
	Query     string          `json:"query"`
	Site      string          `json:"site,omitempty"`
	Results   []google.Result `json:"results"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func (d *DB) GetSearch(query, site string) (*CachedSearch, error) {
	var jsonStr string
	var createdAt, updatedAt time.Time
	err := d.db.QueryRow(
		`SELECT results_json, created_at, updated_at FROM searches
		 WHERE query = ? AND site = ?`, query, site,
	).Scan(&jsonStr, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var results []google.Result
	if err := json.Unmarshal([]byte(jsonStr), &results); err != nil {
		return nil, fmt.Errorf("store: unmarshal results: %w", err)
	}
	return &CachedSearch{Query: query, Site: site, Results: results, CreatedAt: createdAt, UpdatedAt: updatedAt}, nil
}

// PutSearch upserts search results. ON CONFLICT handles duplicate query+site natively.
func (d *DB) PutSearch(query, site string, results []google.Result) error {
	jsonBytes, err := json.Marshal(results)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(
		`INSERT INTO searches (query, site, results_json, created_at, updated_at)
		 VALUES (?, ?, ?, datetime('now'), datetime('now'))
		 ON CONFLICT(query, site) DO UPDATE SET
		   results_json = excluded.results_json,
		   updated_at = datetime('now')`,
		query, site, string(jsonBytes),
	)
	return err
}

// ─── Page cache ─────────────────────────────────────────────────

type CachedPage struct {
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Byline    string    `json:"byline,omitempty"`
	Markdown  string    `json:"markdown"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (d *DB) GetPage(url string) (*CachedPage, error) {
	var p CachedPage
	p.URL = url
	err := d.db.QueryRow(
		`SELECT title, byline, markdown, created_at, updated_at FROM pages WHERE url = ?`, url,
	).Scan(&p.Title, &p.Byline, &p.Markdown, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// PutPage upserts a page. URL is the unique key.
func (d *DB) PutPage(url, title, byline, markdown string) error {
	_, err := d.db.Exec(
		`INSERT INTO pages (url, title, byline, markdown, created_at, updated_at)
		 VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))
		 ON CONFLICT(url) DO UPDATE SET
		   title = excluded.title,
		   byline = excluded.byline,
		   markdown = excluded.markdown,
		   updated_at = datetime('now')`,
		url, title, byline, markdown,
	)
	return err
}

// DeletePage removes a page entry from the cache. Returns nil if the URL
// was not present. Used to purge error-page entries that were saved
// before the error-page filter was added.
func (d *DB) DeletePage(url string) error {
	_, err := d.db.Exec(`DELETE FROM pages WHERE url = ?`, url)
	return err
}

// ─── Stats ──────────────────────────────────────────────────────

func (d *DB) Stats() (searches int, pages int, err error) {
	d.db.QueryRow("SELECT COUNT(*) FROM searches").Scan(&searches)
	d.db.QueryRow("SELECT COUNT(*) FROM pages").Scan(&pages)
	return
}
