// Copyright 2026 Golinky
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	texttemplate "text/template"
	"time"
)

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// serveAPILinks handles GET /.api/links (list all) and POST /.api/links (create/update).
func serveAPILinks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		links, err := db.LoadAll()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		sort.Slice(links, func(i, j int) bool {
			return links[i].Short < links[j].Short
		})
		if links == nil {
			links = []*Link{}
		}
		writeJSON(w, http.StatusOK, links)

	case http.MethodPost:
		var input struct {
			Short string `json:"short"`
			Long  string `json:"long"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
			return
		}
		if input.Short == "" || input.Long == "" {
			writeJSON(w, http.StatusBadRequest, apiError{Error: "short and long required"})
			return
		}
		if !reShortName.MatchString(input.Short) {
			writeJSON(w, http.StatusBadRequest, apiError{Error: "short may only contain letters, numbers, dash, and period"})
			return
		}
		if _, err := texttemplate.New("").Funcs(expandFuncMap).Parse(input.Long); err != nil {
			writeJSON(w, http.StatusBadRequest, apiError{Error: fmt.Sprintf("long contains an invalid template: %v", err)})
			return
		}

		link, err := db.Load(input.Short)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}

		now := time.Now().UTC()
		isNew := link == nil
		if isNew {
			link = &Link{
				Short:   input.Short,
				Created: now,
			}
		}
		link.Short = input.Short
		link.Long = input.Long
		link.LastEdit = now

		if err := db.Save(link); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}

		status := http.StatusOK
		if isNew {
			status = http.StatusCreated
		}
		writeJSON(w, status, link)

	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, apiError{Error: "method not allowed"})
	}
}

// serveAPILink handles GET /.api/links/{name}, PUT /.api/links/{name}, DELETE /.api/links/{name}.
func serveAPILink(w http.ResponseWriter, r *http.Request) {
	short := strings.TrimPrefix(r.URL.Path, "/.api/links/")
	if short == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "short name required"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		link, err := db.Load(short)
		if errors.Is(err, fs.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, apiError{Error: "link not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, link)

	case http.MethodPut:
		var input struct {
			Long string `json:"long"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid JSON body"})
			return
		}
		if input.Long == "" {
			writeJSON(w, http.StatusBadRequest, apiError{Error: "long required"})
			return
		}
		if _, err := texttemplate.New("").Funcs(expandFuncMap).Parse(input.Long); err != nil {
			writeJSON(w, http.StatusBadRequest, apiError{Error: fmt.Sprintf("long contains an invalid template: %v", err)})
			return
		}

		link, err := db.Load(short)
		if errors.Is(err, fs.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, apiError{Error: "link not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}

		link.Long = input.Long
		link.LastEdit = time.Now().UTC()
		if err := db.Save(link); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, link)

	case http.MethodDelete:
		link, err := db.Load(short)
		if errors.Is(err, fs.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, apiError{Error: "link not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		if err := db.Delete(short); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
			return
		}
		deleteLinkStats(link)
		writeJSON(w, http.StatusOK, link)

	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, apiError{Error: "method not allowed"})
	}
}
