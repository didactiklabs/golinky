// Copyright 2026 Golinky
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"log"
	"sync"
	"time"
)

var stats struct {
	mu     sync.Mutex
	clicks ClickStats // short link -> number of times visited
	dirty  ClickStats // dirty identifies short link clicks that have not yet been stored.
}

func initStats() error {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	// Load all stats from the beginning of time
	start := time.Unix(0, 0)
	end := time.Now().UTC()

	clicks, err := db.LoadStats(start, end)
	if err != nil {
		return err
	}

	stats.clicks = clicks
	stats.dirty = make(ClickStats)

	return nil
}

func flushStats() error {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	if len(stats.dirty) == 0 {
		return nil
	}

	if err := db.SaveStats(stats.dirty); err != nil {
		return err
	}
	stats.dirty = make(ClickStats)
	return nil
}

func flushStatsLoop() {
	for {
		if err := flushStats(); err != nil {
			log.Printf("flushing stats: %v", err)
		}
		time.Sleep(time.Minute)
	}
}

func deleteLinkStats(link *Link) {
	stats.mu.Lock()
	delete(stats.clicks, link.Short)
	delete(stats.dirty, link.Short)
	stats.mu.Unlock()
}
