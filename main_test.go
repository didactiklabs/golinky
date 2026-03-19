package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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

func TestExpandLinkSimple(t *testing.T) {
	tests := []struct {
		name string
		long string
		path string
		want string
	}{
		{
			name: "simple URL no path",
			long: "https://example.com",
			path: "",
			want: "https://example.com",
		},
		{
			name: "simple URL with path",
			long: "https://example.com",
			path: "foo",
			want: "https://example.com/foo",
		},
		{
			name: "trailing slash no path",
			long: "https://example.com/",
			path: "",
			want: "https://example.com/",
		},
		{
			name: "trailing slash with path",
			long: "https://example.com/",
			path: "foo/bar",
			want: "https://example.com/foo/bar",
		},
		{
			name: "bare domain without scheme",
			long: "example.com",
			path: "",
			want: "http://example.com",
		},
		{
			name: "bare domain with path",
			long: "example.com",
			path: "page",
			want: "http://example.com/page",
		},
		{
			name: "bare domain with subpath",
			long: "example.com/foo",
			path: "",
			want: "http://example.com/foo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := expandEnv{Now: time.Now(), Path: tt.path}
			got, err := expandLink(tt.long, env)
			if err != nil {
				t.Fatalf("expandLink: %v", err)
			}
			if got.String() != tt.want {
				t.Errorf("expandLink(%q, path=%q) = %q, want %q", tt.long, tt.path, got.String(), tt.want)
			}
		})
	}
}

func TestExpandLinkTemplate(t *testing.T) {
	tests := []struct {
		name string
		long string
		path string
		want string
	}{
		{
			name: "search template no path",
			long: "https://google.com/{{if .Path}}search?q={{QueryEscape .Path}}{{end}}",
			path: "",
			want: "https://google.com/",
		},
		{
			name: "search template with path",
			long: "https://google.com/{{if .Path}}search?q={{QueryEscape .Path}}{{end}}",
			path: "hello world",
			want: "https://google.com/search?q=hello+world",
		},
		{
			name: "path escape",
			long: "https://example.com/{{PathEscape .Path}}",
			path: "foo/bar",
			want: "https://example.com/foo%2Fbar",
		},
		{
			name: "to lower",
			long: "https://example.com/{{ToLower .Path}}",
			path: "HELLO",
			want: "https://example.com/hello",
		},
		{
			name: "to upper",
			long: "https://example.com/{{ToUpper .Path}}",
			path: "hello",
			want: "https://example.com/HELLO",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := expandEnv{Now: time.Now(), Path: tt.path}
			got, err := expandLink(tt.long, env)
			if err != nil {
				t.Fatalf("expandLink: %v", err)
			}
			if got.String() != tt.want {
				t.Errorf("expandLink(%q, path=%q) = %q, want %q", tt.long, tt.path, got.String(), tt.want)
			}
		})
	}
}

func TestExpandLinkQueryParams(t *testing.T) {
	env := expandEnv{
		Now:   time.Now(),
		Path:  "",
		query: url.Values{"ref": {"copilot"}},
	}
	got, err := expandLink("https://example.com", env)
	if err != nil {
		t.Fatalf("expandLink: %v", err)
	}
	if !strings.Contains(got.String(), "ref=copilot") {
		t.Errorf("expected query param ref=copilot in %q", got.String())
	}
}

func TestExpandLinkInvalidTemplate(t *testing.T) {
	env := expandEnv{Now: time.Now(), Path: ""}
	_, err := expandLink("https://example.com/{{.Invalid", env)
	if err == nil {
		t.Error("expected error for invalid template")
	}
}

func TestRegexMatch(t *testing.T) {
	if !regexMatch(`^foo`, "foobar") {
		t.Error("expected match")
	}
	if regexMatch(`^foo`, "barfoo") {
		t.Error("expected no match")
	}
}

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

// --- HTTP handler tests ---

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

// --- Healthcheck tests ---

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

// --- JSON API tests ---

func TestAPIListLinks(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Empty list.
	resp, err := http.Get(ts.URL + "/.api/links")
	if err != nil {
		t.Fatalf("GET /.api/links: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var links []Link
	if err := json.NewDecoder(resp.Body).Decode(&links); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("len(links) = %d, want 0", len(links))
	}
}

func TestAPICreateLink(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	body := strings.NewReader(`{"short":"api-test","long":"https://example.com"}`)
	resp, err := http.Post(ts.URL+"/.api/links", "application/json", body)
	if err != nil {
		t.Fatalf("POST /.api/links: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	var link Link
	if err := json.NewDecoder(resp.Body).Decode(&link); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if link.Short != "api-test" || link.Long != "https://example.com" {
		t.Errorf("link = %+v, want short=api-test long=https://example.com", link)
	}

	// Verify it exists via GET.
	resp2, err := http.Get(ts.URL + "/.api/links/api-test")
	if err != nil {
		t.Fatalf("GET /.api/links/api-test: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("GET status = %d, want %d", resp2.StatusCode, http.StatusOK)
	}
}

func TestAPICreateLinkValidation(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"missing short", `{"long":"https://example.com"}`, http.StatusBadRequest},
		{"missing long", `{"short":"test"}`, http.StatusBadRequest},
		{"invalid short", `{"short":"-bad","long":"https://example.com"}`, http.StatusBadRequest},
		{"invalid template", `{"short":"test","long":"https://example.com/{{.Invalid"}`, http.StatusBadRequest},
		{"invalid json", `not json`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Post(ts.URL+"/.api/links", "application/json", strings.NewReader(tt.body))
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

func TestAPIUpdateLink(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create a link first.
	body := strings.NewReader(`{"short":"update-me","long":"https://old.com"}`)
	resp, err := http.Post(ts.URL+"/.api/links", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	// Update via PUT.
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/.api/links/update-me", strings.NewReader(`{"long":"https://new.com"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var link Link
	if err := json.NewDecoder(resp.Body).Decode(&link); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if link.Long != "https://new.com" {
		t.Errorf("Long = %q, want %q", link.Long, "https://new.com")
	}
}

func TestAPIUpdateLinkNotFound(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/.api/links/nonexistent", strings.NewReader(`{"long":"https://new.com"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestAPIDeleteLink(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create a link.
	body := strings.NewReader(`{"short":"delete-me","long":"https://example.com"}`)
	resp, err := http.Post(ts.URL+"/.api/links", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	// Delete via API.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/.api/links/delete-me", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Verify it's gone.
	resp, err = http.Get(ts.URL + "/.api/links/delete-me")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("after delete, status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestAPIDeleteLinkNotFound(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/.api/links/nonexistent", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestAPIGetLinkNotFound(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.api/links/nonexistent")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestAPIMethodNotAllowed(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// PATCH on collection endpoint.
	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/.api/links", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestAPIUpdateExistingLink(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Create a link.
	body := strings.NewReader(`{"short":"existing","long":"https://old.com"}`)
	resp, err := http.Post(ts.URL+"/.api/links", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	// POST the same short name with new long (update via POST).
	body = strings.NewReader(`{"short":"existing","long":"https://updated.com"}`)
	resp, err = http.Post(ts.URL+"/.api/links", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var link Link
	if err := json.NewDecoder(resp.Body).Decode(&link); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if link.Long != "https://updated.com" {
		t.Errorf("Long = %q, want %q", link.Long, "https://updated.com")
	}
}
