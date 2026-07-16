// Copyright 2026 Golinky
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

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
