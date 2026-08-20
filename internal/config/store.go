package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FilePath is the on-disk location of the persisted configuration.
func FilePath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "status-tui", "config.json")
}

// persistedService mirrors Service.
type persistedService struct {
	Name    string `json:"name"`
	Match   string `json:"match"`
	Cmdline bool   `json:"cmdline,omitempty"`
}

// persisted mirrors Settings with JSON-friendly duration strings.
//
// Charts is written out in full on every save — one explicit true/false per
// chart — so the file doubles as the list of what exists and can be edited by
// hand without guessing at names.
type persisted struct {
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
	Services         []persistedService `json:"services"`
	Charts           map[string]bool    `json:"charts"`
}

// Load reads the persisted configuration, layering it over the defaults. A
// missing file is not an error; an unparsable one is reported but still yields a
// usable default config.
func Load() (*Config, error) {
	s := DefaultSettings()
	path := FilePath()
	if path == "" {
		return &Config{s: s}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{s: s}, nil
		}
		return &Config{s: s}, err
	}
	var p persisted
	if err := json.Unmarshal(raw, &p); err != nil {
		return &Config{s: s}, fmt.Errorf("parse %s: %w", path, err)
	}
	apply(&s, p)
	return &Config{s: s}, nil
}

func apply(s *Settings, p persisted) {
	if p.Host != "" {
		s.Host = p.Host
	}
	if p.Path != "" {
		s.Path = p.Path
	}
	if p.Scheme != "" {
		s.Scheme = p.Scheme
	}
	if d, err := time.ParseDuration(p.Interval); err == nil && d > 0 {
		s.Interval = d
	}
	if d, err := time.ParseDuration(p.SysInterval); err == nil && d > 0 {
		s.SysInterval = d
	}
	if p.History > 0 {
		s.History = p.History
	}
	if p.Mount != "" {
		s.Mount = p.Mount
	}
	s.Iface = p.Iface
	if p.PingMode != "" {
		s.PingMode = PingMode(p.PingMode)
	}
	s.InsecureTLS = p.InsecureTLS
	if p.ServiceThreshold > 0 {
		s.ServiceThreshold = p.ServiceThreshold
	}
	for k, v := range p.Timeouts {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			s.Timeouts[Metric(k)] = d
		}
	}
	// Services are registered before thresholds and visibility are applied, so
	// a service's own entries are not treated as orphans.
	for _, svc := range p.Services {
		if svc.Name == "" {
			continue
		}
		s.AddService(Service{Name: svc.Name, Match: svc.Match, Cmdline: svc.Cmdline})
	}
	for k, v := range p.Thresholds {
		s.Thresholds[ChartID(k)] = v
	}
	for k, v := range p.Charts {
		s.Shown[ChartID(k)] = v
	}
}

// Save writes the configuration to disk, creating parent directories.
func (c *Config) Save() (string, error) {
	path := FilePath()
	if path == "" {
		return "", fmt.Errorf("cannot determine config directory")
	}
	s := c.Snapshot()
	p := persisted{
		Host:             s.Host,
		Path:             s.Path,
		Scheme:           s.Scheme,
		Interval:         s.Interval.String(),
		SysInterval:      s.SysInterval.String(),
		History:          s.History,
		Mount:            s.Mount,
		Iface:            s.Iface,
		PingMode:         string(s.PingMode),
		InsecureTLS:      s.InsecureTLS,
		ServiceThreshold: s.ServiceThreshold,
		Timeouts:         map[string]string{},
		Thresholds:       map[string]float64{},
		Charts:           map[string]bool{},
		Services:         []persistedService{},
	}
	for k, v := range s.Timeouts {
		p.Timeouts[string(k)] = v.String()
	}
	for k, v := range s.Thresholds {
		p.Thresholds[string(k)] = v
	}
	for _, svc := range s.Services {
		p.Services = append(p.Services, persistedService{Name: svc.Name, Match: svc.Match, Cmdline: svc.Cmdline})
	}
	// Every chart gets an explicit entry, including the ones left at the default.
	for _, c := range s.AllCharts() {
		p.Charts[string(c)] = s.IsShown(c)
	}

	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
