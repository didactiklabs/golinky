// Copyright 2026 Golinky
// SPDX-License-Identifier: BSD-3-Clause

// The golinky server runs http://localhost:8080/, a simple shortlink service.
package main

import (
	"context"
	"embed"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	defaultHostname = "go"
)

var (
	sqlitefile = flag.String("sqlitedb", "", "path of SQLite database to store links")
	listen     = flag.String("listen", "localhost:8080", "address to listen on")
	snapshot   = flag.String("snapshot", "", "file path of snapshot file")
)

// LastSnapshot is the data snapshot that will be loaded on startup.
var LastSnapshot []byte

//go:embed static tmpl/*.html
var embeddedFS embed.FS

// db stores short links.
var db *SQLiteDB

func main() {
	flag.Parse()

	if *sqlitefile == "" {
		// Use a persistent database file in the current directory by default
		*sqlitefile = "./golinky.db"
		log.Printf("Using database: %s", *sqlitefile)
	}

	var err error
	if db, err = NewSQLiteDB(*sqlitefile); err != nil {
		log.Fatalf("NewSQLiteDB(%q): %v", *sqlitefile, err)
	}

	if *snapshot != "" {
		if LastSnapshot, err = os.ReadFile(*snapshot); err != nil {
			log.Fatalf("error reading snapshot file %q: %v", *snapshot, err)
		}
	}

	if err := restoreLastSnapshot(); err != nil {
		log.Printf("restoring snapshot: %v", err)
	}

	if err := initStats(); err != nil {
		log.Printf("initializing stats: %v", err)
	}

	// flush stats periodically
	go flushStatsLoop()

	// Setup graceful shutdown
	srv := &http.Server{
		Addr:    *listen,
		Handler: serveHandler(),
	}

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("\nShutting down gracefully...")

		// Flush stats before shutdown
		if err := flushStats(); err != nil {
			log.Printf("Error flushing stats: %v", err)
		}

		// Shutdown server with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	log.Printf("Running golinky on %s ...", *listen)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	log.Println("Server stopped")
}
