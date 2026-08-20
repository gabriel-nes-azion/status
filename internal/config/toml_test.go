package config

import (
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

// nastySettings exercises the values most likely to break a hand-written
// encoder: quotes, backslashes, control characters, non-ASCII and a chart key
// that is not a bare TOML key.
func nastySettings() Settings {
	s := DefaultSettings()
	s.Host = "status.azion.app:8443"
	s.Path = `/health?q=a%20b&x="y"`
	s.Scheme = "http"
	s.Interval = 2500 * time.Millisecond
	s.SysInterval = 250 * time.Millisecond
	s.History = 42
	s.Mount = `/Volumes/My "Disk"\backup`
	s.Iface = "en0"
	s.PingMode = PingTCP
	s.InsecureTLS = true
	s.ServiceThreshold = 12.5
	s.Timeouts[DNS] = 1500 * time.Millisecond

	s.AddService(Service{Name: "nginx", Match: "nginx"})
	s.AddService(Service{Name: "api", Match: `java -jar "api".jar \opt`, Cmdline: true})
	s.AddService(Service{Name: "worker", Match: "w\u00f6rker\tworker", Cmdline: true})
	s.AddService(Service{Name: "ctrl", Match: "a\x01b\x7fc", Cmdline: true})

	s.Thresholds[ServiceChart("nginx")] = 25
	s.Thresholds[MetricChart(DiskIO)] = 0 // explicitly disabled
	s.Shown[MetricChart(Disk)] = false
	s.Shown[ServiceChart("api")] = false
	return s
}

func reload(t *testing.T, doc string) Settings {
	t.Helper()
	s := DefaultSettings()
	var f tomlFile
	if _, err := toml.Decode(doc, &f); err != nil {
		t.Fatalf("decode: %v", err)
	}
	apply(&s, f)
	return s
}

// TestEncodeIsValidTOML is the safety net for writing TOML by hand: whatever the
// encoder produces has to be readable by a real parser.
func TestEncodeIsValidTOML(t *testing.T) {
	for name, s := range map[string]Settings{
		"defaults": DefaultSettings(),
		"nasty":    nastySettings(),
	} {
		var f tomlFile
		if _, err := toml.Decode(encode(s, "/tmp/x.toml"), &f); err != nil {
			t.Errorf("%s: encoder produced unparseable TOML: %v", name, err)
		}
	}
}

// TestRoundTripIsStable checks that loading what was written and writing it again
// reproduces the same document. Text idempotence catches an escape that decodes
// to something other than what went in, which a comparison of only the fields we
// thought to check would miss.
func TestRoundTripIsStable(t *testing.T) {
	first := encode(nastySettings(), "/tmp/x.toml")
	second := encode(reload(t, first), "/tmp/x.toml")
	if first != second {
		t.Errorf("round trip is not stable:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// TestRoundTripKeepsValues spot-checks the values a stability test alone would
// not prove had survived correctly.
func TestRoundTripKeepsValues(t *testing.T) {
	want := nastySettings()
	got := reload(t, encode(want, "/tmp/x.toml"))

	if got.URL() != want.URL() {
		t.Errorf("url = %q, want %q", got.URL(), want.URL())
	}
	if got.Mount != want.Mount {
		t.Errorf("mount = %q, want %q", got.Mount, want.Mount)
	}
	if got.Interval != want.Interval || got.SysInterval != want.SysInterval {
		t.Errorf("intervals = %v/%v", got.Interval, got.SysInterval)
	}
	if got.History != want.History || got.Iface != want.Iface {
		t.Errorf("history/iface = %d/%q", got.History, got.Iface)
	}
	if got.PingMode != want.PingMode || !got.InsecureTLS {
		t.Errorf("ping mode / tls = %v/%v", got.PingMode, got.InsecureTLS)
	}
	if got.ServiceThreshold != want.ServiceThreshold {
		t.Errorf("service default = %v, want %v", got.ServiceThreshold, want.ServiceThreshold)
	}
	if got.Timeouts[DNS] != want.Timeouts[DNS] {
		t.Errorf("dns timeout = %v", got.Timeouts[DNS])
	}
	if len(got.Services) != len(want.Services) {
		t.Fatalf("services = %+v", got.Services)
	}
	for _, w := range want.Services {
		g, ok := got.Service(w.Name)
		if !ok {
			t.Errorf("service %q missing", w.Name)
			continue
		}
		if g.Match != w.Match || g.Cmdline != w.Cmdline {
			t.Errorf("service %q match = %q (cmdline %v), want %q (%v)",
				w.Name, g.Match, g.Cmdline, w.Match, w.Cmdline)
		}
	}
	// A colon-bearing chart key has to survive quoting.
	if th, on := got.Threshold(ServiceChart("nginx")); !on || th != 25 {
		t.Errorf("nginx threshold = %v (set %v)", th, on)
	}
	// An explicitly disabled threshold must not come back as the default.
	if _, on := got.Threshold(MetricChart(DiskIO)); on {
		t.Error("diskio threshold should still be disabled")
	}
	if got.IsShown(MetricChart(Disk)) || got.IsShown(ServiceChart("api")) {
		t.Error("hidden charts came back shown")
	}
	if !got.IsShown(MetricChart(CPU)) {
		t.Error("a shown chart came back hidden")
	}
}

// TestServiceWithoutThresholdUsesFileDefault covers the ordering trap: the
// default has to be read before services are registered, or a service the file
// gives no threshold for picks up the built-in default instead of the file's.
func TestServiceWithoutThresholdUsesFileDefault(t *testing.T) {
	s := reload(t, `
[thresholds]
service_default = 7.5

[[services]]
name = "nginx"
match = "nginx"
`)
	if s.ServiceThreshold != 7.5 {
		t.Fatalf("service default = %v, want 7.5", s.ServiceThreshold)
	}
	if th, on := s.Threshold(ServiceChart("nginx")); !on || th != 7.5 {
		t.Errorf("nginx threshold = %v (set %v), want the file default of 7.5", th, on)
	}
}

// TestPartialFileKeepsDefaults is the promise in the file header: what a file
// omits keeps its default.
func TestPartialFileKeepsDefaults(t *testing.T) {
	s := reload(t, `
[target]
url = "http://localhost:9000/health"

[thresholds]
cpu = 40.0
`)
	if s.URL() != "http://localhost:9000/health" {
		t.Errorf("url = %q", s.URL())
	}
	if th, _ := s.Threshold(MetricChart(CPU)); th != 40 {
		t.Errorf("cpu threshold = %v", th)
	}
	def := DefaultSettings()
	if s.Interval != def.Interval || s.History != def.History || s.Mount != def.Mount {
		t.Errorf("omitted fields lost their defaults: %v %d %q", s.Interval, s.History, s.Mount)
	}
	if th, _ := s.Threshold(MetricChart(TTFB)); th != 300 {
		t.Errorf("omitted threshold = %v, want the default 300", th)
	}
	if !s.IsShown(MetricChart(CPU)) {
		t.Error("a file with no [charts] should show everything")
	}
}

// TestBadTargetURLKeepsDefault: one unusable value must not take the rest of the
// file down with it.
func TestBadTargetURLKeepsDefault(t *testing.T) {
	s := reload(t, `
[target]
url = "ftp://nope"

[sampling]
history = 77
`)
	if s.Host != DefaultSettings().Host {
		t.Errorf("host = %q, want the default kept", s.Host)
	}
	if s.History != 77 {
		t.Errorf("history = %d, want the rest of the file applied", s.History)
	}
}

// TestOrphanThresholdSurvives: removing a service should not silently forget the
// level that was set for it.
func TestOrphanThresholdSurvives(t *testing.T) {
	s := DefaultSettings()
	s.Thresholds[ServiceChart("gone")] = 33

	out := encode(s, "/tmp/x.toml")
	if !strings.Contains(out, `"service:gone" = 33.0`) {
		t.Errorf("orphan threshold not written:\n%s", out)
	}
	if th, on := reload(t, out).Threshold(ServiceChart("gone")); !on || th != 33 {
		t.Errorf("orphan threshold = %v (set %v)", th, on)
	}
}

func TestQuoteEscaping(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", `"plain"`},
		{`a"b`, `"a\"b"`},
		{`a\b`, `"a\\b"`},
		{"a\tb", `"a\tb"`},
		{"a\nb", `"a\nb"`},
		{"a\rb", `"a\rb"`},
		{"a\x01b", `"a\u0001b"`},
		{"a\x7fb", `"a\u007Fb"`},
		// Printable non-ASCII stays as UTF-8: escaping it would be valid TOML but
		// harder to read, and the file is meant to be edited.
		{"caf\u00e9", "\"caf\u00e9\""},
	}
	for _, c := range cases {
		if got := quote(c.in); got != c.want {
			t.Errorf("quote(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestTOMLKeyQuoting(t *testing.T) {
	for in, want := range map[string]string{
		"cpu":           "cpu",
		"service:nginx": `"service:nginx"`,
		"with-dash_ok":  "with-dash_ok",
		"":              `""`,
		"has space":     `"has space"`,
		"dotted.key":    `"dotted.key"`,
	} {
		if got := tomlKey(in); got != want {
			t.Errorf("tomlKey(%q) = %s, want %s", in, got, want)
		}
	}
}

// TestFloatKeysStayFloats: TOML will not read an integer into a float field, so a
// whole-numbered threshold still needs its decimal point.
func TestFloatKeysStayFloats(t *testing.T) {
	out := encode(DefaultSettings(), "/tmp/x.toml")
	if !strings.Contains(out, "cpu = 85.0") {
		t.Errorf("whole threshold written without a decimal point:\n%s", out)
	}
	var f tomlFile
	if _, err := toml.Decode(out, &f); err != nil {
		t.Fatalf("integer-looking float broke the parse: %v", err)
	}
	if f.Thresholds["cpu"] != 85 {
		t.Errorf("cpu = %v", f.Thresholds["cpu"])
	}
}
