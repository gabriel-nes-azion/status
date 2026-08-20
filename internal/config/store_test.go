package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadExplicitPath(t *testing.T) {
	// The default location is somewhere else entirely, so this also proves
	// --conf is honoured rather than merged with the default.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "prod.toml")

	if err := os.WriteFile(path, []byte(`
[target]
url = "https://prod.example.com/health"

[sampling]
checks = "30s"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Path() != path {
		t.Errorf("Path() = %q, want %q", c.Path(), path)
	}
	s := c.Snapshot()
	if s.URL() != "https://prod.example.com/health" {
		t.Errorf("url = %q", s.URL())
	}
	if s.Interval != 30*time.Second {
		t.Errorf("interval = %v", s.Interval)
	}

	// Saving goes back to the same file, not the default location.
	c.Update(func(s *Settings) { s.History = 123 })
	saved, err := c.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved != path {
		t.Errorf("saved to %q, want %q", saved, path)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Snapshot().History; got != 123 {
		t.Errorf("history = %d after reload", got)
	}
}

func TestLoadMissingExplicitPathIsCreatedOnSave(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "nested", "new.toml")

	c, err := Load(path)
	if err != nil {
		t.Fatalf("a missing --conf file must not be an error: %v", err)
	}
	if got := c.Snapshot().Host; got != DefaultSettings().Host {
		t.Errorf("host = %q, want the defaults", got)
	}

	// A first run leaves a file to edit even when nothing was changed.
	written, err := c.SaveIfChanged()
	if err != nil {
		t.Fatalf("SaveIfChanged: %v", err)
	}
	if written != path {
		t.Errorf("wrote %q, want %q", written, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestSaveIfChangedSkipsUntouchedRun(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "c.toml")

	first, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.SaveIfChanged(); err != nil { // creates it
		t.Fatal(err)
	}

	// A run that changes nothing must not rewrite the file, so a hand-edited
	// file keeps its formatting and its timestamp.
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	written, err := c.SaveIfChanged()
	if err != nil {
		t.Fatal(err)
	}
	if written != "" {
		t.Errorf("rewrote %q despite no changes", written)
	}

	// A change is written, and then the next save is a no-op again.
	c.Update(func(s *Settings) { s.Interval = 9 * time.Second })
	if written, err = c.SaveIfChanged(); err != nil || written != path {
		t.Fatalf("SaveIfChanged after a change = %q, %v", written, err)
	}
	if written, err = c.SaveIfChanged(); err != nil || written != "" {
		t.Errorf("second save = %q, %v; want a no-op", written, err)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")

	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Save(); err != nil {
		t.Fatal(err)
	}
	// No temporary file left behind: a save writes one file and only one.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "c.toml" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory holds %v, want just c.toml", names)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("mode = %v, want 0644", perm)
	}
}

func TestUnparsableFileStillStarts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "broken.toml")
	if err := os.WriteFile(path, []byte("[target\nurl = "), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err == nil {
		t.Error("a corrupt file should be reported")
	} else if !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the file: %v", err)
	}
	if c == nil || c.Snapshot().Host != DefaultSettings().Host {
		t.Fatal("a corrupt file must still yield a usable configuration")
	}
	// And it must not be treated as absent, or the next exit would overwrite the
	// file the user is trying to fix.
	written, err := c.SaveIfChanged()
	if err != nil {
		t.Fatal(err)
	}
	if written != "" {
		t.Errorf("overwrote the broken file at %q without any change being made", written)
	}

	// A run that does change something does write — but keeps a copy first, so
	// the hand-edited content survives a typo.
	c.Update(func(s *Settings) { s.History = 55 })
	if written, err = c.SaveIfChanged(); err != nil || written != path {
		t.Fatalf("SaveIfChanged = %q, %v", written, err)
	}
	backup := c.SavedBackup()
	if backup != path+".bak" {
		t.Fatalf("SavedBackup() = %q, want %q", backup, path+".bak")
	}
	kept, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("backup unreadable: %v", err)
	}
	if string(kept) != "[target\nurl = " {
		t.Errorf("backup holds %q, want the original content", kept)
	}
	// The replacement is valid, and a second save does not make another copy.
	reloaded, err := Load(path)
	if err != nil {
		t.Errorf("the replacement file does not parse: %v", err)
	}
	if got := reloaded.Snapshot().History; got != 55 {
		t.Errorf("history = %d after the rescue", got)
	}
	c.Update(func(s *Settings) { s.History = 56 })
	if _, err := c.SaveIfChanged(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak.bak"); err == nil {
		t.Error("a second save made another backup")
	}
}

func TestLegacyJSONIsImportedOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	dir := filepath.Join(home, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, legacyFileName)
	if err := os.WriteFile(legacy, []byte(`{
	  "host": "old.example.com",
	  "scheme": "http",
	  "path": "/legacy",
	  "interval": "17s",
	  "thresholds": {"cpu": 42, "service:nginx": 11},
	  "services": [{"name": "nginx", "match": "nginx"}],
	  "charts": {"disk": false},
	  "hidden": ["ttfb"]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.MigratedFrom() != legacy {
		t.Errorf("MigratedFrom() = %q, want %q", c.MigratedFrom(), legacy)
	}
	s := c.Snapshot()
	if s.URL() != "http://old.example.com/legacy" {
		t.Errorf("url = %q", s.URL())
	}
	if s.Interval != 17*time.Second {
		t.Errorf("interval = %v", s.Interval)
	}
	if th, _ := s.Threshold(MetricChart(CPU)); th != 42 {
		t.Errorf("cpu threshold = %v", th)
	}
	if _, ok := s.Service("nginx"); !ok {
		t.Error("service not imported")
	}
	if th, _ := s.Threshold(ServiceChart("nginx")); th != 11 {
		t.Errorf("service threshold = %v", th)
	}
	if s.IsShown(MetricChart(Disk)) {
		t.Error("charts map not imported")
	}
	if s.IsShown(MetricChart(TTFB)) {
		t.Error("the older hidden list was not imported")
	}

	// The import counts as a change, so exit writes the TOML file.
	written, err := c.SaveIfChanged()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, fileName); written != want {
		t.Errorf("wrote %q, want %q", written, want)
	}
	// Once the TOML exists it wins, and the JSON is never consulted again.
	again, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if again.MigratedFrom() != "" {
		t.Error("legacy file imported a second time")
	}
	if again.Snapshot().URL() != "http://old.example.com/legacy" {
		t.Error("settings did not survive the migration")
	}
	// The legacy file is left alone rather than deleted behind the user's back.
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("legacy file removed: %v", err)
	}
}

func TestExplicitPathDoesNotPickUpLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// A JSON file sitting next to an explicit --conf target must be ignored:
	// a path given on the command line is taken literally.
	if err := os.WriteFile(filepath.Join(dir, legacyFileName),
		[]byte(`{"host":"sneaky.example.com"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(filepath.Join(dir, "mine.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Snapshot().Host; got != DefaultSettings().Host {
		t.Errorf("host = %q, want the defaults", got)
	}
	if c.MigratedFrom() != "" {
		t.Errorf("MigratedFrom() = %q", c.MigratedFrom())
	}
}

func TestDefaultPathUsesXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-example")
	if got, want := DefaultPath(), filepath.Join("/tmp/xdg-example", dirName, fileName); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}
