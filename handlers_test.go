// Copyright 2026 Golinky
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestReShortName(t *testing.T) {
	valid := []string{"foo", "Foo", "foo-bar", "foo.bar", "a1", "test123", "a"}
	for _, s := range valid {
		if !reShortName.MatchString(s) {
			t.Errorf("reShortName should match %q", s)
		}
	}
	invalid := []string{"-foo", ".foo", "", " foo", "foo bar"}
	for _, s := range invalid {
		if reShortName.MatchString(s) {
			t.Errorf("reShortName should not match %q", s)
		}
	}
}

func TestAcceptHTML(t *testing.T) {
	tests := []struct {
		accept string
		want   bool
	}{
		{"text/html", true},
		{"text/html, application/json", true},
		{"TEXT/HTML", true},
		{"application/json", false},
		{"", false},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept", tt.accept)
		got := acceptHTML(r)
		if got != tt.want {
			t.Errorf("acceptHTML(%q) = %v, want %v", tt.accept, got, tt.want)
		}
	}
}

func TestServeSaveAndExport(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create a link via POST.
	resp, err := http.PostForm(ts.URL+"/", url.Values{
		"short": {"test"},
		"long":  {"https://example.com"},
	})
	if err != nil {
		t.Fatalf("POST /: %v", err)
	}
	resp.Body.Close()

	// Export and verify the link is there.
	resp, err = http.Get(ts.URL + "/.export")
	if err != nil {
		t.Fatalf("GET /.export: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var link Link
	if err := json.Unmarshal(body[:len(strings.TrimSpace(string(body)))], &link); err != nil {
		// JSON Lines: first line should be valid JSON.
		lines := strings.Split(strings.TrimSpace(string(body)), "\n")
		if err := json.Unmarshal([]byte(lines[0]), &link); err != nil {
			t.Fatalf("unmarshal export line: %v\nbody: %s", err, body)
		}
	}

	if link.Short != "test" || link.Long != "https://example.com" {
		t.Errorf("exported link = %+v, want short=test long=https://example.com", link)
	}
}

func TestServeSaveValidation(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	tests := []struct {
		name       string
		short      string
		long       string
		wantStatus int
	}{
		{"missing short", "", "https://example.com", http.StatusBadRequest},
		{"missing long", "test", "", http.StatusBadRequest},
		{"invalid short name", "-invalid", "https://example.com", http.StatusBadRequest},
		{"invalid template", "test", "https://example.com/{{.Invalid", http.StatusBadRequest},
		{"valid", "valid", "https://example.com", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.PostForm(ts.URL+"/", url.Values{
				"short": {tt.short},
				"long":  {tt.long},
			})
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestServeRedirect(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create a link.
	resp, err := http.PostForm(ts.URL+"/", url.Values{
		"short": {"gh"},
		"long":  {"https://github.com"},
	})
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	// Follow redirect manually.
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err = client.Get(ts.URL + "/gh")
	if err != nil {
		t.Fatalf("GET /gh: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if loc != "https://github.com" {
		t.Errorf("Location = %q, want %q", loc, "https://github.com")
	}
}

func TestServeRedirectWithPath(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.PostForm(ts.URL+"/", url.Values{
		"short": {"gh"},
		"long":  {"https://github.com/"},
	})
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err = client.Get(ts.URL + "/gh/user/repo")
	if err != nil {
		t.Fatalf("GET /gh/user/repo: %v", err)
	}
	resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc != "https://github.com/user/repo" {
		t.Errorf("Location = %q, want %q", loc, "https://github.com/user/repo")
	}
}

func TestServePlusRedirectToDetail(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + "/gh+")
	if err != nil {
		t.Fatalf("GET /gh+: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if loc != "/.detail/gh" {
		t.Errorf("Location = %q, want %q", loc, "/.detail/gh")
	}
}

func TestServeNotFound(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + "/nonexistent")
	if err != nil {
		t.Fatalf("GET /nonexistent: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestServeDetailJSON(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create a link.
	resp, err := http.PostForm(ts.URL+"/", url.Values{
		"short": {"detail-test"},
		"long":  {"https://example.com"},
	})
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/.detail/detail-test", nil)
	req.Header.Set("Accept", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /.detail: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var link Link
	if err := json.NewDecoder(resp.Body).Decode(&link); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if link.Short != "detail-test" {
		t.Errorf("Short = %q, want %q", link.Short, "detail-test")
	}
}

func TestServeDelete(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create then delete.
	resp, err := http.PostForm(ts.URL+"/", url.Values{
		"short": {"tobedeleted"},
		"long":  {"https://example.com"},
	})
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	resp, err = http.Get(ts.URL + "/.delete/tobedeleted")
	if err != nil {
		t.Fatalf("GET /.delete: %v", err)
	}
	resp.Body.Close()

	// Verify it's gone.
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err = client.Get(ts.URL + "/tobedeleted")
	if err != nil {
		t.Fatalf("GET /tobedeleted: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("after delete, status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestServeHelp(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.help")
	if err != nil {
		t.Fatalf("GET /.help: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestServeAll(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.all")
	if err != nil {
		t.Fatalf("GET /.all: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestServeSearch(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create some links.
	for _, s := range []string{"alpha", "bravo"} {
		resp, _ := http.PostForm(ts.URL+"/", url.Values{
			"short": {s},
			"long":  {"https://" + s + ".com"},
		})
		resp.Body.Close()
	}

	resp, err := http.Get(ts.URL + "/.search?q=alpha")
	if err != nil {
		t.Fatalf("GET /.search: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "alpha") {
		t.Error("search result should contain 'alpha'")
	}

	// Fuzzy match: non-contiguous letters should still find "alpha".
	resp2, err := http.Get(ts.URL + "/.search?q=aph")
	if err != nil {
		t.Fatalf("GET /.search fuzzy: %v", err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body2), "alpha") {
		t.Error("fuzzy search 'aph' should match 'alpha'")
	}
}

func TestServeClickStats(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create a link.
	resp, _ := http.PostForm(ts.URL+"/", url.Values{
		"short": {"clicks"},
		"long":  {"https://example.com"},
	})
	resp.Body.Close()

	// Visit it a few times.
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	for range 3 {
		resp, _ = client.Get(ts.URL + "/clicks")
		resp.Body.Close()
	}

	// Verify stats were incremented.
	stats.mu.Lock()
	count := stats.clicks["clicks"]
	stats.mu.Unlock()

	if count != 3 {
		t.Errorf("clicks = %d, want 3", count)
	}
}

func TestServeHealthz(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("status = %q, want %q", result["status"], "ok")
	}
}
