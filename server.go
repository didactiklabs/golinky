// Copyright 2026 Golinky
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"log"
	"net/http"
	"strings"
	"time"
)

func serveHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.detail/", serveDetail)
	mux.HandleFunc("/.export", serveExport)
	mux.HandleFunc("/.all", serveAll)
	mux.HandleFunc("/.delete/", serveDelete)
	mux.HandleFunc("/.search", serveSearch)
	mux.HandleFunc("/.help", serveHelp)
	mux.HandleFunc("/healthz", serveHealthz)
	mux.HandleFunc("/.api/links", serveAPILinks)
	mux.HandleFunc("/.api/links/", serveAPILink)
	mux.Handle("/.static/", http.StripPrefix("/.", http.FileServer(http.FS(embeddedFS))))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/.") && r.URL.Path != "/healthz" {
			serveGo(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})

	return loggingMiddleware(handler)
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip logging for static assets to reduce noise.
		if strings.HasPrefix(r.URL.Path, "/.static/") {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.statusCode, time.Since(start).Round(time.Microsecond))
	})
}
