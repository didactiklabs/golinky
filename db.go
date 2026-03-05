// Copyright 2024 Golinky
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"database/sql"
	_ "embed"
	"io/fs"
	"net/url"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
) // Link is the structure stored for each go short link.
type Link struct {
	Short    string // the "foo" part of http://go/foo
	Long     string // the target URL or text/template pattern to run
	Created  time.Time
	LastEdit time.Time // when the link was last edited
}

// ClickStats is the number of clicks a set of links have received in a given
// time period. It is keyed by link short name, with values of total clicks.
type ClickStats map[string]int

// linkID returns the normalized ID for a link short name.
func linkID(short string) string {
	id := url.PathEscape(strings.ToLower(short))
	id = strings.ReplaceAll(id, "-", "")
	return id
}

// SQLiteDB stores Links in a SQLite database.
type SQLiteDB struct {
	db *sql.DB
	mu sync.RWMutex
}

//go:embed schema.sql
var sqlSchema string

// NewSQLiteDB returns a new SQLiteDB that stores links in a SQLite database stored at f.
func NewSQLiteDB(f string) (*SQLiteDB, error) {
	db, err := sql.Open("sqlite", f)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}

	if _, err = db.Exec(sqlSchema); err != nil {
		return nil, err
	}

	return &SQLiteDB{db: db}, nil
}

// LoadAll returns all stored Links.
func (s *SQLiteDB) LoadAll() ([]*Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var links []*Link
	rows, err := s.db.Query("SELECT Short, Long, Created, LastEdit FROM Links")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		link := new(Link)
		var created, lastEdit int64
		err := rows.Scan(&link.Short, &link.Long, &created, &lastEdit)
		if err != nil {
			return nil, err
		}
		link.Created = time.Unix(created, 0).In(time.Local)
		link.LastEdit = time.Unix(lastEdit, 0).In(time.Local)
		links = append(links, link)
	}
	return links, rows.Err()
}

// Load returns a Link by its short name.
func (s *SQLiteDB) Load(short string) (*Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	link := new(Link)
	var created, lastEdit int64
	err := s.db.QueryRow("SELECT Short, Long, Created, LastEdit FROM Links WHERE ID = ?", linkID(short)).
		Scan(&link.Short, &link.Long, &created, &lastEdit)
	if err == sql.ErrNoRows {
		return nil, fs.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	link.Created = time.Unix(created, 0).In(time.Local)
	link.LastEdit = time.Unix(lastEdit, 0).In(time.Local)
	return link, nil
}

// Save saves a Link.
func (s *SQLiteDB) Save(link *Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Unix()
	_, err := s.db.Exec(`
		INSERT INTO Links (ID, Short, Long, Created, LastEdit)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (ID) DO UPDATE SET
			Short = excluded.Short,
			Long = excluded.Long,
			LastEdit = excluded.LastEdit
	`, linkID(link.Short), link.Short, link.Long, now, now)
	return err
}

// Delete deletes a link by its short name.
func (s *SQLiteDB) Delete(short string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM Links WHERE ID = ?", linkID(short))
	return err
}

// LoadStats loads click stats for a given time period.
func (s *SQLiteDB) LoadStats(start, end time.Time) (ClickStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make(ClickStats)

	// Get all stats with their corresponding link short names
	rows, err := s.db.Query(`
		SELECT Links.Short, SUM(Stats.Clicks) as TotalClicks
		FROM Stats
		JOIN Links ON Stats.ID = Links.ID
		WHERE Stats.Created >= ? AND Stats.Created < ?
		GROUP BY Links.Short
	`, start.Unix(), end.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var short string
		var clicks int
		if err := rows.Scan(&short, &clicks); err != nil {
			return nil, err
		}
		stats[short] = clicks
	}
	return stats, rows.Err()
}

// SaveStats saves click stats.
func (s *SQLiteDB) SaveStats(stats ClickStats) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Unix()
	for short, clicks := range stats {
		if clicks == 0 {
			continue
		}
		_, err := s.db.Exec(`
			INSERT INTO Stats (ID, Created, Clicks)
			VALUES (?, ?, ?)
		`, linkID(short), now, clicks)
		if err != nil {
			return err
		}
	}
	return nil
}
