// Copyright 2026 Golinky
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

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
