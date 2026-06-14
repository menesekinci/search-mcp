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

// maxAgeDays: 0 = force live (skip cache), -1 = default (7 days), >0 = specific TTL in days.
func (d *DB) GetSearch(query, site string, maxAgeDays int) (*CachedSearch, error) {
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
	cached := &CachedSearch{Query: query, Site: site, Results: results, CreatedAt: createdAt, UpdatedAt: updatedAt}

	// TTL check
	if maxAgeDays == 0 {
		return nil, nil // force live
	}
	days := maxAgeDays
	if days < 0 {
		days = 7 // default 7 days
	}
	if time.Since(cached.UpdatedAt).Hours() > float64(days*24) {
		return nil, nil // expired
	}
	return cached, nil
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

// GetPage returns a cached page if it exists and is within TTL.
// maxAgeDays: 0 = force live (skip cache), -1 = default (7 days), >0 = specific TTL in days.
func (d *DB) GetPage(url string, maxAgeDays int) (*CachedPage, error) {
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

	// TTL check
	if maxAgeDays == 0 {
		return nil, nil // force live
	}
	days := maxAgeDays
	if days < 0 {
		days = 7 // default 7 days
	}
	if time.Since(p.UpdatedAt).Hours() > float64(days*24) {
		return nil, nil // expired
	}
	return &p, nil
}

// GetPageStale returns the most recent cached page regardless of TTL.
// Used as a fallback when a live fetch fails for an expired entry.
func (d *DB) GetPageStale(url string) (*CachedPage, error) {
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

// ─── Maintenance ────────────────────────────────────────────────

// PurgeExpired deletes search and page entries older than maxAgeDays.
func (d *DB) PurgeExpired(maxAgeDays int) (searchesPurged, pagesPurged int, err error) {
	res, err := d.db.Exec(`DELETE FROM searches WHERE updated_at < datetime('now', ?)`,
		fmt.Sprintf("-%d days", maxAgeDays))
	if err != nil {
		return 0, 0, err
	}
	n, _ := res.RowsAffected()
	searchesPurged = int(n)

	res, err = d.db.Exec(`DELETE FROM pages WHERE updated_at < datetime('now', ?)`,
		fmt.Sprintf("-%d days", maxAgeDays))
	if err != nil {
		return searchesPurged, 0, err
	}
	n, _ = res.RowsAffected()
	pagesPurged = int(n)
	return
}

// ─── Stats ──────────────────────────────────────────────────────

// CacheStats holds aggregate cache statistics.
type CacheStats struct {
	Searches       int        `json:"searches"`
	Pages          int        `json:"pages"`
	TotalSizeBytes int64      `json:"total_size_bytes"`
	OldestSearch   *time.Time `json:"oldest_search,omitempty"`
	NewestPage     *time.Time `json:"newest_page,omitempty"`
}

// CacheStats returns aggregate statistics about the cache database.
func (d *DB) CacheStats() (*CacheStats, error) {
	var s CacheStats

	if err := d.db.QueryRow("SELECT COUNT(*) FROM searches").Scan(&s.Searches); err != nil {
		return nil, fmt.Errorf("cache stats: count searches: %w", err)
	}
	if err := d.db.QueryRow("SELECT COUNT(*) FROM pages").Scan(&s.Pages); err != nil {
		return nil, fmt.Errorf("cache stats: count pages: %w", err)
	}

	var pageCount, pageSize int64
	if err := d.db.QueryRow("PRAGMA page_count").Scan(&pageCount); err != nil {
		return nil, fmt.Errorf("cache stats: pragma page_count: %w", err)
	}
	if err := d.db.QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
		return nil, fmt.Errorf("cache stats: pragma page_size: %w", err)
	}
	s.TotalSizeBytes = pageCount * pageSize

	var oldestSearchStr, newestPageStr sql.NullString
	if err := d.db.QueryRow("SELECT MIN(updated_at) FROM searches").Scan(&oldestSearchStr); err != nil {
		return nil, fmt.Errorf("cache stats: min search updated_at: %w", err)
	}
	if oldestSearchStr.Valid {
		t, err := time.Parse("2006-01-02 15:04:05", oldestSearchStr.String)
		if err != nil {
			return nil, fmt.Errorf("cache stats: parse oldest search: %w", err)
		}
		s.OldestSearch = &t
	}
	if err := d.db.QueryRow("SELECT MAX(updated_at) FROM pages").Scan(&newestPageStr); err != nil {
		return nil, fmt.Errorf("cache stats: max page updated_at: %w", err)
	}
	if newestPageStr.Valid {
		t, err := time.Parse("2006-01-02 15:04:05", newestPageStr.String)
		if err != nil {
			return nil, fmt.Errorf("cache stats: parse newest page: %w", err)
		}
		s.NewestPage = &t
	}

	return &s, nil
}

func (d *DB) Stats() (searches int, pages int, err error) {
	if err = d.db.QueryRow("SELECT COUNT(*) FROM searches").Scan(&searches); err != nil {
		return 0, 0, fmt.Errorf("stats: count searches: %w", err)
	}
	if err = d.db.QueryRow("SELECT COUNT(*) FROM pages").Scan(&pages); err != nil {
		return 0, 0, fmt.Errorf("stats: count pages: %w", err)
	}
	return
}
