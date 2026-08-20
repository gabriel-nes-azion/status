package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestSnapshotIsIndependent(t *testing.T) {
	c := Default()
	snap := c.Snapshot()
	c.Update(func(s *Settings) {
		s.Host = "changed.example"
		s.Thresholds[MetricChart(CPU)] = 1
		s.Timeouts[DNS] = time.Hour
	})
	if snap.Host == "changed.example" {
		t.Error("snapshot host followed a later mutation")
	}
	if v := snap.Thresholds[MetricChart(CPU)]; v == 1 {
		t.Error("snapshot threshold map aliases live state")
	}
	if v := snap.Timeouts[DNS]; v == time.Hour {
		t.Error("snapshot timeout map aliases live state")
	}
}

func TestURL(t *testing.T) {
	cases := []struct {
		scheme, host, path, want string
	}{
		{"https", "status.azion.app", "/", "https://status.azion.app/"},
		{"http", "localhost:8080", "/health", "http://localhost:8080/health"},
		{"https", "example.com", "health", "https://example.com/health"},
		{"https", "example.com", "", "https://example.com/"},
	}
	for _, c := range cases {
		s := Settings{Scheme: c.scheme, Host: c.host, Path: c.path}
		if got := s.URL(); got != c.want {
			t.Errorf("URL() = %q, want %q", got, c.want)
		}
	}
}

func TestThresholdZeroDisables(t *testing.T) {
	s := DefaultSettings()
	if _, on := s.Threshold(MetricChart(CPU)); !on {
		t.Error("default cpu threshold should be enabled")
	}
	s.Thresholds[MetricChart(CPU)] = 0
	if _, on := s.Threshold(MetricChart(CPU)); on {
		t.Error("a zero threshold must read as disabled")
	}
	if _, on := s.Threshold(ChartID("nope")); on {
		t.Error("an unknown chart must read as disabled")
	}
}

func TestTimeoutFallback(t *testing.T) {
	s := DefaultSettings()
	s.Timeouts[DNS] = 0
	if got := s.Timeout(DNS); got != 5*time.Second {
		t.Errorf("Timeout with a zero entry = %v, want the 5s fallback", got)
	}
	if got := s.Timeout(Metric("nope")); got != 5*time.Second {
		t.Errorf("Timeout for an unknown metric = %v", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	c.Update(func(s *Settings) {
		s.Host = "example.com"
		s.Path = "/health"
		s.Scheme = "http"
		s.Interval = 12 * time.Second
		s.SysInterval = 250 * time.Millisecond
		s.History = 42
		s.Mount = "/System/Volumes/Data"
		s.Iface = "en0"
		s.PingMode = PingTCP
		s.InsecureTLS = true
		s.Timeouts[TTFB] = 1500 * time.Millisecond
		s.Thresholds[MetricChart(Net)] = 250 << 20
		s.Shown[MetricChart(DiskIO)] = false
		s.Shown[MetricChart(Ping)] = false
		s.AddService(Service{Name: "nginx", Match: "nginx"})
		s.AddService(Service{Name: "api", Match: "java -jar api.jar", Cmdline: true})
		s.Shown[ServiceChart("api")] = false
	})
	path, err := c.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if want := filepath.Join(dir, "status-tui", "config.toml"); path != want {
		t.Errorf("saved to %s, want %s", path, want)
	}

	loaded, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, want := loaded.Snapshot(), c.Snapshot()
	if got.Host != want.Host || got.Path != want.Path || got.Scheme != want.Scheme {
		t.Errorf("target = %+v", got)
	}
	if got.Interval != want.Interval || got.SysInterval != want.SysInterval {
		t.Errorf("intervals = %v / %v", got.Interval, got.SysInterval)
	}
	if got.History != want.History || got.Mount != want.Mount || got.Iface != want.Iface {
		t.Errorf("sources = %d %s %s", got.History, got.Mount, got.Iface)
	}
	if got.PingMode != PingTCP || !got.InsecureTLS {
		t.Errorf("flags = %s %v", got.PingMode, got.InsecureTLS)
	}
	if got.Timeouts[TTFB] != 1500*time.Millisecond {
		t.Errorf("ttfb timeout = %v", got.Timeouts[TTFB])
	}
	if got.Thresholds[MetricChart(Net)] != 250<<20 {
		t.Errorf("net threshold = %v", got.Thresholds[MetricChart(Net)])
	}
	if got.IsShown(MetricChart(DiskIO)) || got.IsShown(MetricChart(Ping)) || !got.IsShown(MetricChart(CPU)) {
		t.Errorf("visibility set = %v", got.Shown)
	}
	if n := got.VisibleCount(ScreenMain); n != len(Order)-2 {
		t.Errorf("visible count = %d, want %d", n, len(Order)-2)
	}
	if len(got.Services) != 2 {
		t.Fatalf("services = %+v", got.Services)
	}
	api, ok := got.Service("api")
	if !ok || api.Match != "java -jar api.jar" || !api.Cmdline {
		t.Errorf("api service = %+v (found %v)", api, ok)
	}
	if got.IsShown(ServiceChart("api")) {
		t.Error("api chart should have been restored as hidden")
	}
	if !got.IsShown(ServiceChart("nginx")) {
		t.Error("nginx chart should be shown")
	}
	if th, on := got.Threshold(ServiceChart("nginx")); !on || th != got.ServiceThreshold {
		t.Errorf("nginx threshold = %v (set %v)", th, on)
	}
	if n := got.VisibleCount(ScreenServices); n != 1 {
		t.Errorf("services screen shows %d, want 1 (api is hidden)", n)
	}
}

// TestSaveWritesEveryChartExplicitly pins the file format promise: the config is
// editable by hand, so it must list every chart with a true/false rather than
// only the ones that were toggled.
func TestSaveWritesEveryChartExplicitly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	c.Update(func(s *Settings) {
		s.AddService(Service{Name: "nginx", Match: "nginx"})
		s.Shown[MetricChart(Disk)] = false
	})
	path, err := c.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	var file tomlFile
	if _, err := toml.DecodeFile(path, &file); err != nil {
		t.Fatalf("saved file is not valid TOML: %v", err)
	}
	if len(file.Charts) != len(Order)+1 {
		t.Errorf("charts has %d entries, want %d (every built-in plus the service)",
			len(file.Charts), len(Order)+1)
	}
	for _, m := range Order {
		shown, ok := file.Charts[string(m)]
		if !ok {
			t.Errorf("%s missing from charts", m)
			continue
		}
		if m == Disk && shown {
			t.Error("disk should be written as false")
		}
		if m != Disk && !shown {
			t.Errorf("%s should be written as true", m)
		}
	}
	if _, ok := file.Charts["service:nginx"]; !ok {
		t.Error("the service chart is missing from charts")
	}
	if len(file.Services) != 1 || file.Services[0].Name != "nginx" {
		t.Errorf("services = %+v", file.Services)
	}
}

func TestNormaliseServiceName(t *testing.T) {
	cases := map[string]string{
		"nginx":                  "nginx",
		"Code Helper (Renderer)": "code-helper--renderer",
		"My Worker":              "my-worker",
		"postgres:14":            "postgres-14",
		"  spaced  ":             "spaced",
		"redis-server":           "redis-server",
	}
	for in, want := range cases {
		if got := NormaliseServiceName(in); got != want {
			t.Errorf("NormaliseServiceName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveChartAcceptsServices(t *testing.T) {
	s := DefaultSettings()
	s.AddService(Service{Name: "nginx", Match: "nginx"})

	if got, err := s.ResolveChart("cpu"); err != nil || got != MetricChart(CPU) {
		t.Errorf("cpu -> %v, %v", got, err)
	}
	if got, err := s.ResolveChart("nginx"); err != nil || got != ServiceChart("nginx") {
		t.Errorf("nginx -> %v, %v", got, err)
	}
	// The namespaced form is accepted too, so a config value round-trips.
	if got, err := s.ResolveChart("service:nginx"); err != nil || got != ServiceChart("nginx") {
		t.Errorf("service:nginx -> %v, %v", got, err)
	}
	if _, err := s.ResolveChart("nope"); err == nil {
		t.Error("an unknown name should be rejected")
	}
	// A service cannot shadow a built-in.
	s.AddService(Service{Name: "cpu", Match: "whatever"})
	if got, _ := s.ResolveChart("cpu"); got != MetricChart(CPU) {
		t.Errorf("a service named cpu shadowed the built-in: %v", got)
	}
	if ScreenOfHelper(s, ServiceChart("cpu")) != ScreenServices {
		t.Error("the service chart should still live on the services screen")
	}
}

// ScreenOfHelper keeps the assertion above readable.
func ScreenOfHelper(s Settings, c ChartID) Screen { return s.ScreenOf(c) }

func TestSectionsForDropsEmptyGroups(t *testing.T) {
	s := DefaultSettings()
	if got := len(s.SectionsFor(ScreenMain)); got != 2 {
		t.Fatalf("main screen has %d sections, want 2", got)
	}
	if got := s.SectionsFor(ScreenServices); len(got) != 0 {
		t.Errorf("no services configured should yield no sections, got %+v", got)
	}

	for _, m := range MachineMetrics {
		s.Shown[MetricChart(m)] = false
	}
	secs := s.SectionsFor(ScreenMain)
	if len(secs) != 1 || secs[0].Name != "Network" {
		t.Errorf("hiding a whole group should drop its heading, got %+v", secs)
	}
	s.Shown[MetricChart(NetworkMetrics[0])] = false
	secs = s.SectionsFor(ScreenMain)
	if len(secs) != 1 || len(secs[0].Charts) != len(NetworkMetrics)-1 {
		t.Errorf("a partly hidden group should keep its visible members, got %+v", secs)
	}
	for _, m := range Order {
		s.Shown[MetricChart(m)] = false
	}
	if got := s.SectionsFor(ScreenMain); len(got) != 0 {
		t.Errorf("nothing visible should yield no sections, got %+v", got)
	}
}

func TestSnapshotShownIsIndependent(t *testing.T) {
	c := Default()
	snap := c.Snapshot()
	c.Update(func(s *Settings) { s.Shown[MetricChart(CPU)] = false })
	if !snap.IsShown(MetricChart(CPU)) {
		t.Error("snapshot visibility map aliases live state")
	}
	c2 := Default()
	snap2 := c2.Snapshot()
	c2.Update(func(s *Settings) { s.AddService(Service{Name: "nginx"}) })
	if len(snap2.Services) != 0 {
		t.Error("snapshot service slice aliases live state")
	}
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := Load("")
	if err != nil {
		t.Fatalf("a missing config file must not be an error: %v", err)
	}
	if got := c.Snapshot().Host; got != DefaultSettings().Host {
		t.Errorf("host = %q", got)
	}
}

func TestLoadBrokenFileStillYieldsDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "status-tui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "status-tui", "config.toml"), []byte("[target\nnope"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load("")
	if err == nil {
		t.Error("a corrupt file should be reported")
	}
	if c == nil || c.Snapshot().Host != DefaultSettings().Host {
		t.Error("a corrupt file must still yield a usable default config")
	}
}

func TestEveryMetricHasATitle(t *testing.T) {
	for _, m := range Order {
		if Titles[m] == "" {
			t.Errorf("metric %s has no panel title", m)
		}
	}
	for _, m := range Probes {
		if _, ok := DefaultSettings().Timeouts[m]; !ok {
			t.Errorf("probe %s has no default timeout", m)
		}
	}
}

func TestParseTarget(t *testing.T) {
	cases := []struct {
		in                 string
		scheme, host, path string
	}{
		{"status.azion.app", "https", "status.azion.app", "/"},
		{"status.azion.app/health", "https", "status.azion.app", "/health"},
		{"https://status.azion.app/", "https", "status.azion.app", "/"},
		{"http://localhost:8080", "http", "localhost:8080", "/"},
		{"https://example.com/a?b=c", "https", "example.com", "/a?b=c"},
		{"  example.com  ", "https", "example.com", "/"},
	}
	for _, c := range cases {
		got, err := ParseTarget(c.in)
		if err != nil {
			t.Errorf("ParseTarget(%q) failed: %v", c.in, err)
			continue
		}
		if got.Scheme != c.scheme || got.Host != c.host || got.Path != c.path {
			t.Errorf("ParseTarget(%q) = %+v, want %s/%s/%s", c.in, got, c.scheme, c.host, c.path)
		}
	}

	for _, bad := range []string{"", "   ", "ftp://example.com", "http://"} {
		if got, err := ParseTarget(bad); err == nil {
			t.Errorf("ParseTarget(%q) should have failed, got %+v", bad, got)
		}
	}
}
