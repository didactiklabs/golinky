// Copyright 2026 Golinky
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/sahilm/fuzzy"
)

var reShortName = regexp.MustCompile(`^\w[\w\-\.]*$`)

func serveHome(w http.ResponseWriter, r *http.Request, short string) {
	var clicks []visitData

	stats.mu.Lock()
	for short, numClicks := range stats.clicks {
		clicks = append(clicks, visitData{
			Short:     short,
			NumClicks: numClicks,
		})
	}
	stats.mu.Unlock()

	sort.Slice(clicks, func(i, j int) bool {
		if clicks[i].NumClicks != clicks[j].NumClicks {
			return clicks[i].NumClicks > clicks[j].NumClicks
		}
		return clicks[i].Short < clicks[j].Short
	})
	if len(clicks) > 200 {
		clicks = clicks[:200]
	}

	homeTmpl.Execute(w, homeData{
		Short:  short,
		Clicks: clicks,
	})
}

func serveAll(w http.ResponseWriter, _ *http.Request) {
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

	searchTmpl.Execute(w, links)
}

func serveGo(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		switch r.Method {
		case "GET":
			serveHome(w, r, "")
		case "POST":
			serveSave(w, r)
		}
		return
	}

	short, remainder, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")

	// redirect {name}+ links to /.detail/{name}
	if strings.HasSuffix(short, "+") {
		http.Redirect(w, r, "/.detail/"+strings.TrimSuffix(short, "+"), http.StatusFound)
		return
	}

	link, err := db.Load(short)
	if errors.Is(err, fs.ErrNotExist) {
		// Trim common punctuation from the end and try again.
		if s := strings.TrimRight(short, ".,()[]{}"); short != s {
			short = s
			link, err = db.Load(short)
		}
	}

	if errors.Is(err, fs.ErrNotExist) {
		w.WriteHeader(http.StatusNotFound)
		serveHome(w, r, short)
		return
	}
	if err != nil {
		log.Printf("serving %q: %v", short, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stats.mu.Lock()
	if stats.clicks == nil {
		stats.clicks = make(ClickStats)
	}
	stats.clicks[link.Short]++
	if stats.dirty == nil {
		stats.dirty = make(ClickStats)
	}
	stats.dirty[link.Short]++
	stats.mu.Unlock()

	env := expandEnv{Now: time.Now().UTC(), Path: remainder, query: r.URL.Query()}
	target, err := expandLink(link.Long, env)
	if err != nil {
		log.Printf("expanding %q: %v", link.Long, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Location", target.String())
	w.WriteHeader(http.StatusFound)
}

func acceptHTML(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html")
}

func serveDetail(w http.ResponseWriter, r *http.Request) {
	short := strings.TrimPrefix(r.URL.Path, "/.detail/")

	link, err := db.Load(short)
	if errors.Is(err, fs.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if short != link.Short {
		http.Redirect(w, r, "/.detail/"+link.Short, http.StatusFound)
		return
	}
	if err != nil {
		log.Printf("serving detail %q: %v", short, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !acceptHTML(r) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.Encode(link)
		return
	}

	detailTmpl.Execute(w, detailData{Link: link})
}

func serveHelp(w http.ResponseWriter, _ *http.Request) {
	helpTmpl.Execute(w, nil)
}

// linkSource adapts a slice of links to the fuzzy.Source interface, matching
// against both the short name and the destination URL.
type linkSource []*Link

func (l linkSource) String(i int) string { return l[i].Short + " " + l[i].Long }
func (l linkSource) Len() int            { return len(l) }

func serveSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	links, err := db.LoadAll()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var filtered []*Link
	if strings.TrimSpace(query) == "" {
		// No query: list every link, sorted alphabetically.
		filtered = links
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].Short < filtered[j].Short
		})
	} else {
		// Fuzzy match, results are already ordered by relevance score.
		matches := fuzzy.FindFrom(query, linkSource(links))
		filtered = make([]*Link, len(matches))
		for i, m := range matches {
			filtered[i] = links[m.Index]
		}
	}
	searchTmpl.Execute(w, filtered)
}

func serveDelete(w http.ResponseWriter, r *http.Request) {
	short := strings.TrimPrefix(r.URL.Path, "/.delete/")
	if short == "" {
		http.Error(w, "short required", http.StatusBadRequest)
		return
	}

	link, err := db.Load(short)
	if errors.Is(err, fs.ErrNotExist) {
		http.NotFound(w, r)
		return
	}

	if err := db.Delete(short); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	deleteLinkStats(link)

	deleteTmpl.Execute(w, deleteData{
		Short: link.Short,
		Long:  link.Long,
	})
}

// serveHealthz responds with 200 OK for health checks.
func serveHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func serveSave(w http.ResponseWriter, r *http.Request) {
	short, long := r.FormValue("short"), r.FormValue("long")
	if short == "" || long == "" {
		http.Error(w, "short and long required", http.StatusBadRequest)
		return
	}
	if !reShortName.MatchString(short) {
		http.Error(w, "short may only contain letters, numbers, dash, and period", http.StatusBadRequest)
		return
	}
	if _, err := texttemplate.New("").Funcs(expandFuncMap).Parse(long); err != nil {
		http.Error(w, fmt.Sprintf("long contains an invalid template: %v", err), http.StatusBadRequest)
		return
	}

	link, err := db.Load(short)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	if link == nil {
		link = &Link{
			Short:   short,
			Created: now,
		}
	}
	link.Short = short
	link.Long = long
	link.LastEdit = now

	if err := db.Save(link); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if acceptHTML(r) {
		successTmpl.Execute(w, homeData{Short: short})
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(link)
	}
}
