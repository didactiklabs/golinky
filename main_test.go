// Copyright 2026 Golinky
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net/http/httptest"
	"testing"
)

// setupTestServer sets up a test HTTP server with a temporary database.
func setupTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	tdb := newTestDB(t)
	db = tdb

	// Reset stats.
	stats.mu.Lock()
	stats.clicks = make(ClickStats)
	stats.dirty = make(ClickStats)
	stats.mu.Unlock()

	return httptest.NewServer(serveHandler())
}
