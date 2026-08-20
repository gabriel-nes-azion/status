// Package config holds every user-tunable value of the dashboard, plus the
// chart identifiers shared by the collectors, the probes and the UI.
package config

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Metric identifies one of the built-in charts. These identifiers are also the
// names accepted by /threshold and /timeout, so they double as user-facing
// labels.
type Metric string

const (
	CPU     Metric = "cpu"
	Mem     Metric = "mem"
	Disk    Metric = "disk"
	DiskIO  Metric = "diskio"
	Net     Metric = "net"
	DNS     Metric = "dns"
	Ping    Metric = "ping"
	TTFB    Metric = "ttfb"
	Request Metric = "request"
)

// Screen is one page of the dashboard. Charts are split across screens because
// nine system charts and an unbounded list of services cannot share one screen,
// but every chart keeps collecting whichever screen is in front.
type Screen int

const (
	ScreenMain Screen = iota
	ScreenServices
)

// Screens is the switching order of Shift+Tab.
var Screens = []Screen{ScreenMain, ScreenServices}

func (s Screen) String() string {
	if s == ScreenServices {
		return "services"
	}
	return "main"
}

// Next returns the screen Shift+Tab moves to.
func (s Screen) Next() Screen { return Screen((int(s) + 1) % len(Screens)) }

// ParseScreen resolves a user-supplied screen name.
func ParseScreen(s string) (Screen, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "main", "1", "system":
		return ScreenMain, nil
	case "services", "service", "2", "svc":
		return ScreenServices, nil
	}
	return ScreenMain, fmt.Errorf("unknown screen %q (main or services)", s)
}

// ChartID identifies any chart, built-in or discovered service. Built-in charts
// use their metric name; services are namespaced so a service called "cpu"
// cannot collide with the built-in one.
type ChartID string

const servicePrefix = "service:"

// MetricChart is the chart id of a built-in metric.
func MetricChart(m Metric) ChartID { return ChartID(m) }

// ServiceChart is the chart id of a monitored service.
func ServiceChart(name string) ChartID { return ChartID(servicePrefix + name) }

// IsService reports whether the chart tracks a service rather than a built-in.
func (c ChartID) IsService() bool { return strings.HasPrefix(string(c), servicePrefix) }

// ServiceName is the service this chart tracks, empty for built-ins.
func (c ChartID) ServiceName() string {
	if !c.IsService() {
		return ""
	}
	return strings.TrimPrefix(string(c), servicePrefix)
}

// Metric returns the built-in metric this chart tracks.
func (c ChartID) Metric() (Metric, bool) {
	if c.IsService() {
		return "", false
	}
	for _, m := range Order {
		if Metric(c) == m {
			return m, true
		}
	}
	return "", false
}

// MachineMetrics and NetworkMetrics are the built-in charts of the main screen,
// split by the question they answer: local pressure versus remote reachability.
var (
	MachineMetrics = []Metric{CPU, Mem, Disk, DiskIO}
	NetworkMetrics = []Metric{Net, Ping, DNS, TTFB, Request}
)

// Order is every built-in metric in display order.
var Order = append(append([]Metric{}, MachineMetrics...), NetworkMetrics...)

// Probes are the network checks driven by the configurable check interval.
var Probes = []Metric{DNS, Ping, TTFB, Request}

// Titles are the short labels of the built-in charts.
var Titles = map[Metric]string{
	CPU:     "CPU",
	Mem:     "MEMORY",
	Disk:    "DISK USAGE",
	DiskIO:  "DISK I/O",
	Net:     "THROUGHPUT",
	DNS:     "DNS LOOKUP",
	Ping:    "PING",
	TTFB:    "TTFB",
	Request: "REQUEST",
}

// Section groups charts under a heading on one screen.
type Section struct {
	Name   string
	Screen Screen
	Charts []ChartID
}

func chartsOf(ms []Metric) []ChartID {
	out := make([]ChartID, 0, len(ms))
	for _, m := range ms {
		out = append(out, MetricChart(m))
	}
	return out
}

// Service is a process group whose CPU share is charted. Match is a
// case-insensitive substring; it is tested against the process name and, when
// Cmdline is set, against the full command line too — which is what it takes to
// tell two JVMs or two python workers apart.
type Service struct {
	Name    string
	Match   string
	Cmdline bool
}

// PingMode selects the transport used by the latency probe.
type PingMode string

const (
	PingICMP PingMode = "icmp"
	PingTCP  PingMode = "tcp"
	PingAuto PingMode = "auto"
)

// Settings is the plain value type of the configuration. It is copied freely:
// probe goroutines each get their own snapshot, so a /host typed mid-check
// cannot change the target under a running probe.
type Settings struct {
	Host        string
	Path        string
	Scheme      string
	Interval    time.Duration
	SysInterval time.Duration
	History     int
	Mount       string
	Iface       string
	PingMode    PingMode
	InsecureTLS bool
	Timeouts    map[Metric]time.Duration
	Thresholds  map[ChartID]float64

	// Services are the process groups discovered by /discover or added by hand.
	Services []Service
	// Shown records each chart's visibility. A chart missing from the map is
	// shown: a new service or a new built-in appears rather than hiding.
	Shown map[ChartID]bool
	// ServiceThreshold is the CPU share a newly added service alerts at.
	ServiceThreshold float64
}

// DefaultSettings returns the built-in configuration values.
func DefaultSettings() Settings {
	return Settings{
		Host:        "status.azion.app",
		Path:        "/",
		Scheme:      "https",
		Interval:    5 * time.Second,
		SysInterval: time.Second,
		History:     600,
		Mount:       "/",
		Iface:       "",
		PingMode:    PingAuto,
		Timeouts: map[Metric]time.Duration{
			DNS:     2 * time.Second,
			Ping:    2 * time.Second,
			TTFB:    10 * time.Second,
			Request: 10 * time.Second,
		},
		Thresholds: map[ChartID]float64{
			MetricChart(CPU):     85,                // percent
			MetricChart(Mem):     90,                // percent
			MetricChart(Disk):    90,                // percent
			MetricChart(DiskIO):  100 * 1024 * 1024, // bytes/s
			MetricChart(Net):     100 * 1024 * 1024, // bytes/s
			MetricChart(DNS):     100,               // ms
			MetricChart(Ping):    100,               // ms
			MetricChart(TTFB):    300,               // ms
			MetricChart(Request): 800,               // ms
		},
		Services:         nil,
		Shown:            map[ChartID]bool{},
		ServiceThreshold: 50, // percent of total CPU capacity
	}
}

// clone deep-copies the maps and slices so a snapshot can never alias live state.
func (s Settings) clone() Settings {
	out := s
	out.Timeouts = make(map[Metric]time.Duration, len(s.Timeouts))
	out.Thresholds = make(map[ChartID]float64, len(s.Thresholds))
	out.Shown = make(map[ChartID]bool, len(s.Shown))
	out.Services = append([]Service(nil), s.Services...)
	for k, v := range s.Timeouts {
		out.Timeouts[k] = v
	}
	for k, v := range s.Thresholds {
		out.Thresholds[k] = v
	}
	for k, v := range s.Shown {
		out.Shown[k] = v
	}
	return out
}

// URL is the target the HTTP probes request.
func (s Settings) URL() string {
	path := s.Path
	if path == "" || path[0] != '/' {
		path = "/" + path
	}
	return fmt.Sprintf("%s://%s%s", s.Scheme, s.Host, path)
}

// Timeout returns the configured timeout for m, falling back to 5s.
func (s Settings) Timeout(m Metric) time.Duration {
	if d, ok := s.Timeouts[m]; ok && d > 0 {
		return d
	}
	return 5 * time.Second
}

// Threshold returns the alert threshold for a chart and whether one is enabled.
// A threshold of zero means "no alert for this chart".
func (s Settings) Threshold(c ChartID) (float64, bool) {
	v, ok := s.Thresholds[c]
	return v, ok && v > 0
}

// IsShown reports whether a chart is displayed. Charts absent from the map are
// shown, so adding one never hides it.
func (s Settings) IsShown(c ChartID) bool {
	v, ok := s.Shown[c]
	return !ok || v
}

// AllCharts is every chart in display order, built-ins then services.
func (s Settings) AllCharts() []ChartID {
	out := make([]ChartID, 0, len(Order)+len(s.Services))
	out = append(out, chartsOf(Order)...)
	for _, svc := range s.Services {
		out = append(out, ServiceChart(svc.Name))
	}
	return out
}

// AllSections is every section of a screen, hidden charts included. The picker
// needs this; the renderer wants SectionsFor.
func (s Settings) AllSections(screen Screen) []Section {
	switch screen {
	case ScreenServices:
		charts := make([]ChartID, 0, len(s.Services))
		for _, svc := range s.Services {
			charts = append(charts, ServiceChart(svc.Name))
		}
		return []Section{{Name: "Services", Screen: ScreenServices, Charts: charts}}
	default:
		return []Section{
			{Name: "Machine", Screen: ScreenMain, Charts: chartsOf(MachineMetrics)},
			{Name: "Network", Screen: ScreenMain, Charts: chartsOf(NetworkMetrics)},
		}
	}
}

// SectionsFor returns the sections of a screen holding only visible charts,
// dropping any section left empty.
func (s Settings) SectionsFor(screen Screen) []Section {
	all := s.AllSections(screen)
	out := make([]Section, 0, len(all))
	for _, sec := range all {
		vis := make([]ChartID, 0, len(sec.Charts))
		for _, c := range sec.Charts {
			if s.IsShown(c) {
				vis = append(vis, c)
			}
		}
		if len(vis) > 0 {
			out = append(out, Section{Name: sec.Name, Screen: sec.Screen, Charts: vis})
		}
	}
	return out
}

// VisibleCount is how many charts a screen currently shows.
func (s Settings) VisibleCount(screen Screen) int {
	n := 0
	for _, sec := range s.SectionsFor(screen) {
		n += len(sec.Charts)
	}
	return n
}

// ScreenOf reports which screen a chart lives on.
func (s Settings) ScreenOf(c ChartID) Screen {
	if c.IsService() {
		return ScreenServices
	}
	return ScreenMain
}

// ChartTitle is the panel label of any chart.
func (s Settings) ChartTitle(c ChartID) string {
	if m, ok := c.Metric(); ok {
		return Titles[m]
	}
	if c.IsService() {
		return strings.ToUpper(c.ServiceName())
	}
	return string(c)
}

// Service looks up a configured service by name, case-insensitively.
func (s Settings) Service(name string) (Service, bool) {
	for _, svc := range s.Services {
		if strings.EqualFold(svc.Name, name) {
			return svc, true
		}
	}
	return Service{}, false
}

// ResolveChart maps a user-supplied name to a chart id, accepting both a
// built-in metric name and a service name.
func (s Settings) ResolveChart(name string) (ChartID, error) {
	want := strings.ToLower(strings.TrimSpace(name))
	want = strings.TrimPrefix(want, servicePrefix)
	for _, m := range Order {
		if string(m) == want {
			return MetricChart(m), nil
		}
	}
	if svc, ok := s.Service(want); ok {
		return ServiceChart(svc.Name), nil
	}
	return "", fmt.Errorf("unknown chart %q", name)
}

// ChartNames lists every name ResolveChart accepts, for completion.
func (s Settings) ChartNames() []string {
	out := make([]string, 0, len(Order)+len(s.Services))
	for _, m := range Order {
		out = append(out, string(m))
	}
	for _, svc := range s.Services {
		out = append(out, svc.Name)
	}
	return out
}

// AddService registers a service, replacing any existing one of the same name
// and giving it the default CPU threshold. Call it inside Config.Update.
func (s *Settings) AddService(svc Service) {
	svc.Name = NormaliseServiceName(svc.Name)
	if svc.Match == "" {
		svc.Match = svc.Name
	}
	for i, existing := range s.Services {
		if strings.EqualFold(existing.Name, svc.Name) {
			s.Services[i] = svc
			return
		}
	}
	s.Services = append(s.Services, svc)
	sort.SliceStable(s.Services, func(i, j int) bool { return s.Services[i].Name < s.Services[j].Name })
	if _, ok := s.Thresholds[ServiceChart(svc.Name)]; !ok {
		s.Thresholds[ServiceChart(svc.Name)] = s.ServiceThreshold
	}
}

// RemoveService drops a service and everything keyed off it.
func (s *Settings) RemoveService(name string) bool {
	for i, svc := range s.Services {
		if !strings.EqualFold(svc.Name, name) {
			continue
		}
		s.Services = append(s.Services[:i], s.Services[i+1:]...)
		delete(s.Thresholds, ServiceChart(svc.Name))
		delete(s.Shown, ServiceChart(svc.Name))
		return true
	}
	return false
}

// NormaliseServiceName makes a discovered process name usable as an identifier.
func NormaliseServiceName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, s)
	return strings.Trim(s, "-")
}

// Config is the shared, mutable holder of Settings. The UI goroutine mutates it
// through Update while probe goroutines read snapshots of it.
type Config struct {
	mu sync.RWMutex
	s  Settings
}

// Default returns a Config holding the built-in defaults.
func Default() *Config { return &Config{s: DefaultSettings()} }

// Snapshot returns an independent copy for a reader to use without locking.
func (c *Config) Snapshot() Settings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.s.clone()
}

// Update runs fn against the live settings while holding the write lock.
func (c *Config) Update(fn func(*Settings)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(&c.s)
}

// Replace swaps in a whole new set of settings.
func (c *Config) Replace(s Settings) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.s = s.clone()
}
