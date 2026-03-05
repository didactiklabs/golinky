package main

import (
	"io/fs"
	"os"
	"testing"
	"time"
)

// newTestDB creates a temporary SQLite database for testing.
func newTestDB(t *testing.T) *SQLiteDB {
	t.Helper()
	f := t.TempDir() + "/test.db"
	db, err := NewSQLiteDB(f)
	if err != nil {
		t.Fatalf("NewSQLiteDB: %v", err)
	}
	t.Cleanup(func() { os.Remove(f) })
	return db
}

func TestLinkID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"foo", "foo"},
		{"Foo", "foo"},
		{"FOO", "foo"},
		{"Foo-Bar", "foobar"},
		{"foo-bar-baz", "foobarbaz"},
		{"foo.bar", "foo.bar"},
		{"Hello-World", "helloworld"},
	}
	for _, tt := range tests {
		got := linkID(tt.input)
		if got != tt.want {
			t.Errorf("linkID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSaveAndLoad(t *testing.T) {
	tdb := newTestDB(t)

	link := &Link{
		Short: "test",
		Long:  "https://example.com",
	}
	if err := tdb.Save(link); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := tdb.Load("test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Short != "test" {
		t.Errorf("Short = %q, want %q", got.Short, "test")
	}
	if got.Long != "https://example.com" {
		t.Errorf("Long = %q, want %q", got.Long, "https://example.com")
	}
}

func TestLoadCaseInsensitive(t *testing.T) {
	tdb := newTestDB(t)

	link := &Link{Short: "MyLink", Long: "https://example.com"}
	if err := tdb.Save(link); err != nil {
		t.Fatalf("Save: %v", err)
	}

	for _, name := range []string{"MyLink", "mylink", "MYLINK", "myLINK"} {
		got, err := tdb.Load(name)
		if err != nil {
			t.Errorf("Load(%q): %v", name, err)
			continue
		}
		if got.Short != "MyLink" {
			t.Errorf("Load(%q).Short = %q, want %q", name, got.Short, "MyLink")
		}
	}
}

func TestLoadHyphenInsensitive(t *testing.T) {
	tdb := newTestDB(t)

	link := &Link{Short: "meeting-notes", Long: "https://example.com/notes"}
	if err := tdb.Save(link); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := tdb.Load("meetingnotes")
	if err != nil {
		t.Fatalf("Load(meetingnotes): %v", err)
	}
	if got.Short != "meeting-notes" {
		t.Errorf("Short = %q, want %q", got.Short, "meeting-notes")
	}
}

func TestLoadNotExist(t *testing.T) {
	tdb := newTestDB(t)

	_, err := tdb.Load("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent link")
	}
	if err != fs.ErrNotExist {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestSaveUpdate(t *testing.T) {
	tdb := newTestDB(t)

	link := &Link{Short: "test", Long: "https://example.com"}
	if err := tdb.Save(link); err != nil {
		t.Fatalf("Save: %v", err)
	}

	link.Long = "https://updated.com"
	if err := tdb.Save(link); err != nil {
		t.Fatalf("Save (update): %v", err)
	}

	got, err := tdb.Load("test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Long != "https://updated.com" {
		t.Errorf("Long = %q, want %q", got.Long, "https://updated.com")
	}
}

func TestDelete(t *testing.T) {
	tdb := newTestDB(t)

	link := &Link{Short: "todelete", Long: "https://example.com"}
	if err := tdb.Save(link); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := tdb.Delete("todelete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := tdb.Load("todelete")
	if err != fs.ErrNotExist {
		t.Errorf("after delete, Load err = %v, want fs.ErrNotExist", err)
	}
}

func TestDeleteNonExistent(t *testing.T) {
	tdb := newTestDB(t)

	if err := tdb.Delete("nope"); err != nil {
		t.Errorf("Delete nonexistent: %v", err)
	}
}

func TestLoadAll(t *testing.T) {
	tdb := newTestDB(t)

	links := []*Link{
		{Short: "alpha", Long: "https://a.com"},
		{Short: "bravo", Long: "https://b.com"},
		{Short: "charlie", Long: "https://c.com"},
	}
	for _, l := range links {
		if err := tdb.Save(l); err != nil {
			t.Fatalf("Save(%q): %v", l.Short, err)
		}
	}

	all, err := tdb.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("LoadAll returned %d links, want 3", len(all))
	}
}

func TestLoadAllEmpty(t *testing.T) {
	tdb := newTestDB(t)

	all, err := tdb.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("LoadAll returned %d links, want 0", len(all))
	}
}

func TestSaveAndLoadStats(t *testing.T) {
	tdb := newTestDB(t)

	link := &Link{Short: "popular", Long: "https://example.com"}
	if err := tdb.Save(link); err != nil {
		t.Fatalf("Save link: %v", err)
	}

	clicks := ClickStats{"popular": 42}
	if err := tdb.SaveStats(clicks); err != nil {
		t.Fatalf("SaveStats: %v", err)
	}

	start := time.Unix(0, 0)
	end := time.Now().Add(time.Hour)

	loaded, err := tdb.LoadStats(start, end)
	if err != nil {
		t.Fatalf("LoadStats: %v", err)
	}
	if loaded["popular"] != 42 {
		t.Errorf("clicks[popular] = %d, want 42", loaded["popular"])
	}
}

func TestSaveStatsSkipsZero(t *testing.T) {
	tdb := newTestDB(t)

	link := &Link{Short: "zero", Long: "https://example.com"}
	if err := tdb.Save(link); err != nil {
		t.Fatalf("Save link: %v", err)
	}

	clicks := ClickStats{"zero": 0}
	if err := tdb.SaveStats(clicks); err != nil {
		t.Fatalf("SaveStats: %v", err)
	}

	start := time.Unix(0, 0)
	end := time.Now().Add(time.Hour)
	loaded, err := tdb.LoadStats(start, end)
	if err != nil {
		t.Fatalf("LoadStats: %v", err)
	}
	if loaded["zero"] != 0 {
		t.Errorf("clicks[zero] = %d, want 0", loaded["zero"])
	}
}
