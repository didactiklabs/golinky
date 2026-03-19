// Copyright 2024 Golinky
// SPDX-License-Identifier: BSD-3-Clause

// The golinky server runs http://localhost:8080/, a simple shortlink service.
package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	texttemplate "text/template"
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

var stats struct {
	mu     sync.Mutex
	clicks ClickStats // short link -> number of times visited
	dirty  ClickStats // dirty identifies short link clicks that have not yet been stored.
}

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

var (
	homeTmpl    *template.Template
	detailTmpl  *template.Template
	successTmpl *template.Template
	deleteTmpl  *template.Template
	searchTmpl  *template.Template
	helpTmpl    *template.Template
)

type visitData struct {
	Short     string
	NumClicks int
}

type homeData struct {
	Short  string
	Long   string
	Clicks []visitData
}

type deleteData struct {
	Short string
	Long  string
}

func init() {
	homeTmpl = newTemplate("base.html", "home.html")
	detailTmpl = newTemplate("base.html", "detail.html")
	successTmpl = newTemplate("base.html", "success.html")
	deleteTmpl = newTemplate("base.html", "delete.html")
	searchTmpl = newTemplate("base.html", "search.html")
	helpTmpl = newTemplate("base.html", "help.html")
}

var tmplFuncs = template.FuncMap{
	"go": func() string {
		return defaultHostname
	},
}

func newTemplate(files ...string) *template.Template {
	if len(files) == 0 {
		return nil
	}
	tf := make([]string, 0, len(files))
	for _, f := range files {
		tf = append(tf, "tmpl/"+f)
	}
	t := template.New(files[0]).Funcs(tmplFuncs)
	return template.Must(t.ParseFS(embeddedFS, tf...))
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

type detailData struct {
	Link *Link
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

func serveSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	links, err := db.LoadAll()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Simple search by short name or long URL
	var filtered []*Link
	for _, link := range links {
		if strings.Contains(strings.ToLower(link.Short), strings.ToLower(query)) ||
			strings.Contains(strings.ToLower(link.Long), strings.ToLower(query)) {
			filtered = append(filtered, link)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Short < filtered[j].Short
	})
	searchTmpl.Execute(w, filtered)
}

type expandEnv struct {
	Now   time.Time
	Path  string
	query url.Values
}

var expandFuncMap = texttemplate.FuncMap{
	"PathEscape":  url.PathEscape,
	"QueryEscape": url.QueryEscape,
	"TrimPrefix":  strings.TrimPrefix,
	"TrimSuffix":  strings.TrimSuffix,
	"ToLower":     strings.ToLower,
	"ToUpper":     strings.ToUpper,
	"Match":       regexMatch,
}

func regexMatch(pattern string, s string) bool {
	b, _ := regexp.MatchString(pattern, s)
	return b
}

func expandLink(long string, env expandEnv) (*url.URL, error) {
	if !strings.Contains(long, "{{") {
		// default behavior is to append remaining path to long URL
		if strings.HasSuffix(long, "/") {
			long += "{{.Path}}"
		} else {
			long += "{{with .Path}}/{{.}}{{end}}"
		}
	}
	tmpl, err := texttemplate.New("").Funcs(expandFuncMap).Parse(long)
	if err != nil {
		return nil, err
	}
	buf := new(bytes.Buffer)
	if err := tmpl.Execute(buf, env); err != nil {
		return nil, err
	}

	u, err := url.Parse(buf.String())
	if err != nil {
		return nil, err
	}

	// Add http:// scheme if missing so bare domains like "example.com" work.
	if u.Scheme == "" && u.Host == "" && !strings.HasPrefix(u.Path, "/") {
		u, err = url.Parse("http://" + buf.String())
		if err != nil {
			return nil, err
		}
	}

	// add query parameters from original request
	if len(env.query) > 0 {
		query := u.Query()
		for key, values := range env.query {
			for _, v := range values {
				query.Add(key, v)
			}
		}
		u.RawQuery = query.Encode()
	}

	return u, nil
}

var reShortName = regexp.MustCompile(`^\w[\w\-\.]*$`)

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

// --- JSON API ---

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
