package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	dirName  = "status-tui"
	fileName = "config.toml"
	// legacyFileName is the JSON file earlier versions wrote. It is read once so
	// an upgrade does not silently lose a saved configuration; it is never
	// written back.
	legacyFileName = "config.json"
	// serviceDefaultKey is the entry in [thresholds] that is not a chart: the
	// level given to a service when it is first added.
	serviceDefaultKey = "service_default"
)

// DefaultPath is where the configuration lives when --conf is not given.
func DefaultPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, dirName, fileName)
}

// Path is the file this configuration was loaded from and will be saved to.
func (c *Config) Path() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.path
}

// tomlService mirrors Service in the file.
type tomlService struct {
	Name    string `toml:"name"`
	Match   string `toml:"match"`
	Cmdline bool   `toml:"cmdline"`
}

// tomlFile is the on-disk shape. Every field is optional: a partial file is a
// valid file, and what it omits keeps its default.
type tomlFile struct {
	Target struct {
		URL         string `toml:"url"`
		PingMode    string `toml:"ping_mode"`
		InsecureTLS bool   `toml:"insecure_tls"`
	} `toml:"target"`
	Sampling struct {
		Checks  string `toml:"checks"`
		System  string `toml:"system"`
		History int    `toml:"history"`
	} `toml:"sampling"`
	Sources struct {
		Mount     string `toml:"mount"`
		Interface string `toml:"interface"`
	} `toml:"sources"`
	Timeouts   map[string]string  `toml:"timeouts"`
	Thresholds map[string]float64 `toml:"thresholds"`
	Charts     map[string]bool    `toml:"charts"`
	Services   []tomlService      `toml:"services"`
}

// Load reads a configuration file, layering it over the defaults. An empty path
// means the default location.
//
// A missing file is not an error: it yields the defaults, marked so that the
// first save creates it. An unparsable file is reported, and still yields a
// usable configuration rather than refusing to start — a dashboard that will not
// open because of a stray comma is worse than one on defaults.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	s := DefaultSettings()
	c := &Config{s: s, path: path, baseline: s}

	if path == "" {
		return c, fmt.Errorf("cannot determine a config directory; changes will not be saved")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return c, err
		}
		// One-time migration: read the JSON file the earlier version wrote, but
		// only when looking at the default location and only if it is there.
		if migrated, ok := loadLegacy(path, &s); ok {
			c.s, c.baseline = s, s
			c.migratedFrom = migrated
		}
		return c, nil
	}

	var f tomlFile
	if _, err := toml.Decode(string(raw), &f); err != nil {
		// The file is there, it just cannot be read. Marking it as existing keeps
		// an unchanged run from overwriting the file the user is trying to fix,
		// and salvage makes a changed run keep a copy of it.
		c.exists = true
		c.salvage = true
		return c, fmt.Errorf("parse %s: %w", path, err)
	}
	apply(&s, f)
	c.s, c.baseline, c.exists = s, s, true
	return c, nil
}

// MigratedFrom is the legacy file a load picked settings up from, empty when
// there was none. The caller reports it so the move is visible.
func (c *Config) MigratedFrom() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.migratedFrom
}

func apply(s *Settings, f tomlFile) {
	if f.Target.URL != "" {
		if t, err := ParseTarget(f.Target.URL); err == nil {
			s.Scheme, s.Host, s.Path = t.Scheme, t.Host, t.Path
		}
	}
	if f.Target.PingMode != "" {
		switch mode := PingMode(f.Target.PingMode); mode {
		case PingAuto, PingICMP, PingTCP:
			s.PingMode = mode
		}
	}
	s.InsecureTLS = f.Target.InsecureTLS

	if d, err := time.ParseDuration(f.Sampling.Checks); err == nil && d > 0 {
		s.Interval = d
	}
	if d, err := time.ParseDuration(f.Sampling.System); err == nil && d > 0 {
		s.SysInterval = d
	}
	if f.Sampling.History > 0 {
		s.History = f.Sampling.History
	}
	if f.Sources.Mount != "" {
		s.Mount = f.Sources.Mount
	}
	s.Iface = f.Sources.Interface

	for k, v := range f.Timeouts {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			s.Timeouts[Metric(k)] = d
		}
	}
	// The default service threshold is read first: AddService falls back to it
	// for a service the file gives no explicit threshold for, so it has to be in
	// place before any service is registered.
	if v, ok := f.Thresholds[serviceDefaultKey]; ok && v > 0 {
		s.ServiceThreshold = v
	}
	// Services come next, so their thresholds and visibility land on a chart
	// that already exists.
	for _, svc := range f.Services {
		if svc.Name == "" {
			continue
		}
		s.AddService(Service{Name: svc.Name, Match: svc.Match, Cmdline: svc.Cmdline})
	}
	for k, v := range f.Thresholds {
		if k == serviceDefaultKey {
			continue
		}
		s.Thresholds[ChartID(k)] = v
	}
	for k, v := range f.Charts {
		s.Shown[ChartID(k)] = v
	}
}

// loadLegacy reads the JSON file a previous version wrote, next to where the
// TOML file would be. It reports the path it read, if any.
func loadLegacy(tomlPath string, s *Settings) (string, bool) {
	if filepath.Base(tomlPath) != fileName {
		return "", false // a --conf path is taken literally, no guessing
	}
	path := filepath.Join(filepath.Dir(tomlPath), legacyFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	if err := applyLegacyJSON(s, raw); err != nil {
		return "", false
	}
	return path, true
}

// Save writes the configuration and returns the path written.
func (c *Config) Save() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}

// SaveIfChanged writes only when something differs from what was loaded, or when
// the file does not exist yet. Called on exit: an unchanged run should leave the
// file's timestamp alone, and a first run should still leave a file to edit.
//
// The returned path is empty when nothing needed writing.
func (c *Config) SaveIfChanged() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exists && reflect.DeepEqual(c.baseline, c.s) {
		return "", nil
	}
	return c.saveLocked()
}

func (c *Config) saveLocked() (string, error) {
	if c.path == "" {
		return "", fmt.Errorf("no config path")
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return "", err
	}

	// A file that could not be parsed is copied aside before being replaced: the
	// user hand-edited it, and a stray bracket should not cost them the content.
	if c.salvage {
		if backup, err := backupFile(c.path); err != nil {
			return "", fmt.Errorf("keeping a copy of the unreadable %s: %w", c.path, err)
		} else if backup != "" {
			c.savedBackup = backup
		}
		c.salvage = false
	}

	body := encode(c.s, c.path)

	// Written through a temporary file in the same directory and renamed over
	// the target, so an interrupted save cannot leave a half-written config
	// where a good one used to be.
	tmp, err := os.CreateTemp(filepath.Dir(c.path), "."+filepath.Base(c.path)+".*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename has happened

	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, c.path); err != nil {
		return "", err
	}

	c.baseline = c.s.clone()
	c.exists = true
	return c.path, nil
}

// legacyJSON mirrors the JSON file earlier versions wrote.
type legacyJSON struct {
	Host             string             `json:"host"`
	Path             string             `json:"path"`
	Scheme           string             `json:"scheme"`
	Interval         string             `json:"interval"`
	SysInterval      string             `json:"sys_interval"`
	History          int                `json:"history"`
	Mount            string             `json:"mount"`
	Iface            string             `json:"iface"`
	PingMode         string             `json:"ping_mode"`
	InsecureTLS      bool               `json:"insecure_tls"`
	Timeouts         map[string]string  `json:"timeouts"`
	Thresholds       map[string]float64 `json:"thresholds"`
	ServiceThreshold float64            `json:"service_threshold"`
	Services         []tomlService      `json:"services"`
	Charts           map[string]bool    `json:"charts"`
	Hidden           []string           `json:"hidden"`
}

// applyLegacyJSON reads the old format into settings. It is deliberately
// separate from the TOML path: this only has to keep working, not stay pretty.
func applyLegacyJSON(s *Settings, raw []byte) error {
	var p legacyJSON
	if err := unmarshalJSON(raw, &p); err != nil {
		return err
	}
	f := tomlFile{}
	f.Target.PingMode = p.PingMode
	f.Target.InsecureTLS = p.InsecureTLS
	f.Sampling.Checks = p.Interval
	f.Sampling.System = p.SysInterval
	f.Sampling.History = p.History
	f.Sources.Mount = p.Mount
	f.Sources.Interface = p.Iface
	f.Timeouts = p.Timeouts
	f.Thresholds = p.Thresholds
	f.Charts = p.Charts
	f.Services = p.Services
	if p.Host != "" {
		scheme := p.Scheme
		if scheme == "" {
			scheme = "https"
		}
		f.Target.URL = scheme + "://" + p.Host + p.Path
	}
	if p.ServiceThreshold > 0 {
		if f.Thresholds == nil {
			f.Thresholds = map[string]float64{}
		}
		f.Thresholds[serviceDefaultKey] = p.ServiceThreshold
	}
	apply(s, f)
	// The oldest format listed only what was hidden.
	for _, k := range p.Hidden {
		s.Shown[ChartID(strings.TrimSpace(k))] = false
	}
	return nil
}

// SavedBackup is the copy a save made of an unreadable configuration file, empty
// when there was none. The caller reports it so the copy is not a surprise.
func (c *Config) SavedBackup() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.savedBackup
}

// backupFile copies path to path.bak, returning the backup path. A missing
// source is not an error: there is simply nothing to keep.
func backupFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	backup := path + ".bak"
	if err := os.WriteFile(backup, raw, 0o644); err != nil {
		return "", err
	}
	return backup, nil
}

// unmarshalJSON is a seam so the legacy reader keeps its own import.
func unmarshalJSON(raw []byte, v any) error { return json.Unmarshal(raw, v) }
