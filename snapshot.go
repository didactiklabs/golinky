// Copyright 2026 Golinky
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"sort"
)

func serveExport(w http.ResponseWriter, _ *http.Request) {
	if err := flushStats(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	links, err := db.LoadAll()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sort.Slice(links, func(i, j int) bool {
		return links[i].Short < links[j].Short
	})
	encoder := json.NewEncoder(w)
	for _, link := range links {
		if err := encoder.Encode(link); err != nil {
			panic(http.ErrAbortHandler)
		}
	}
}

func restoreLastSnapshot() error {
	if len(LastSnapshot) == 0 {
		return nil
	}

	bs := bufio.NewScanner(bytes.NewReader(LastSnapshot))
	var restored int
	for bs.Scan() {
		link := new(Link)
		if err := json.Unmarshal(bs.Bytes(), link); err != nil {
			return err
		}
		if link.Short == "" {
			continue
		}
		_, err := db.Load(link.Short)
		if err == nil {
			continue // exists
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := db.Save(link); err != nil {
			return err
		}
		restored++
	}
	if restored > 0 {
		log.Printf("Restored %v links.", restored)
	}
	return bs.Err()
}
