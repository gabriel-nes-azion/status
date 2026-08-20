package ui

import (
	"math"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"status/internal/config"
	"status/internal/metrics"
)

// seed fills every series with a synthetic wave, plus a burst of breaches and
// failures near the end so the alert rendering is exercised.
func seed(m *Model, n int) {
	base := map[config.ChartID]float64{
		config.MetricChart(config.CPU): 45, config.MetricChart(config.Mem): 70,
		config.MetricChart(config.Disk): 45, config.MetricChart(config.DiskIO): 8 << 20,
		config.MetricChart(config.Net): 2 << 20, config.MetricChart(config.DNS): 40,
		config.MetricChart(config.Ping): 30, config.MetricChart(config.TTFB): 120,
		config.MetricChart(config.Request): 220,
	}
	now := time.Now()
	for i := 0; i < n; i++ {
		wave := 1 + 0.6*math.Sin(float64(i)/6)
		for k, b := range base {
			v := b * wave
			okSample := true
			if k == config.MetricChart(config.Ping) && i > n-8 && i%3 == 0 {
				okSample = false // simulate a flapping check
			}
			if k == config.MetricChart(config.TTFB) && i > n-20 {
				v = 900 // simulate a threshold breach
			}
			m.series[k].Append(metrics.Sample{
				At: now.Add(time.Duration(i-n) * time.Second), Value: v, OK: okSample,
			})
		}
	}
	m.detail[config.MetricChart(config.CPU)] = "8 cores · load 2.31 1.98 1.75"
	m.detail[config.MetricChart(config.Mem)] = "6.5G / 8.0G"
	m.detail[config.MetricChart(config.Disk)] = "/: 103G / 228G"
	m.detail[config.MetricChart(config.DiskIO)] = "r 4.3M/s · w 2.0M/s"
	m.detail[config.MetricChart(config.Net)] = "all · ↓ 686K/s · ↑ 459K/s"
	m.detail[config.MetricChart(config.DNS)] = "pure-go · 2 addr · 191.251.119.234"
	m.detail[config.MetricChart(config.TTFB)] = "dns 2.56ms · conn 16.4ms · tls 48.0ms"
	m.detail[config.MetricChart(config.Request)] = "204 · HTTP/1.1 · 0B · 191.251.119.229:443"
	m.errmsg[config.MetricChart(config.Ping)] = "timeout"
}

func newSized(t *testing.T, w, h int) *Model {
	t.Helper()
	m := New(config.Default())
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func TestViewFitsTerminal(t *testing.T) {
	for _, size := range []struct{ w, h int }{
		{120, 40}, {100, 30}, {80, 24}, {200, 60}, {60, 20}, {40, 14},
		{132, 60}, {34, 13}, {34, 16}, {300, 100},
	} {
		m := newSized(t, size.w, size.h)
		seed(m, 400)
		out := m.View()
		lines := strings.Split(out, "\n")
		if len(lines) > size.h {
			t.Errorf("%dx%d: view is %d lines, terminal has %d", size.w, size.h, len(lines), size.h)
		}
		for i, l := range lines {
			if w := width(l); w > size.w {
				t.Errorf("%dx%d: line %d is %d cells wide", size.w, size.h, i, w)
			}
		}
	}
}

func TestChartGeometry(t *testing.T) {
	s := metrics.NewSeries(50)
	for i := 0; i < 50; i++ {
		s.Append(metrics.Sample{Value: float64(i * 2), OK: true})
	}
	ch := chart{samples: s.Tail(40), width: 40, height: 5, max: 100, threshold: 80}
	rows := ch.render()
	if len(rows) != 5 {
		t.Fatalf("want 5 rows, got %d", len(rows))
	}
	for i, r := range rows {
		if w := width(r); w != 40 {
			t.Errorf("row %d: want width 40, got %d", i, w)
		}
	}
	// The threshold guide must land inside the chart.
	if tr := ch.thresholdRow(); tr < 0 || tr >= 5 {
		t.Errorf("threshold row %d out of range", tr)
	}
}

func TestChartMarksFailuresAndGaps(t *testing.T) {
	s := metrics.NewSeries(10)
	s.Append(metrics.Sample{Value: 50, OK: true})
	s.Append(metrics.Sample{OK: false})
	ch := chart{samples: s.Tail(10), width: 10, height: 3, max: 100}
	out := strings.Join(ch.render(), "\n")
	if !strings.ContainsRune(out, outageGlyph) {
		t.Error("failed sample did not render an outage glyph")
	}
	if !strings.ContainsRune(out, noDataGlyph) {
		t.Error("missing history did not render a no-data glyph")
	}
}

func TestDumpView(t *testing.T) {
	m := newSized(t, 132, 40)
	seed(m, 400)
	t.Log("\n" + m.View())
}

func TestDumpViewNarrow(t *testing.T) {
	m := newSized(t, 84, 26)
	seed(m, 400)
	t.Log("\n" + m.View())
}

func TestDumpSuggestions(t *testing.T) {
	m := newSized(t, 132, 40)
	seed(m, 400)
	m.input.SetValue("/t")
	m.refreshSuggestions()
	t.Log("\n" + m.View())
}

func TestDumpOverlay(t *testing.T) {
	m := newSized(t, 132, 44)
	seed(m, 400)
	m.input.SetValue("/config")
	m.submit()
	t.Log("\n" + m.View())
}

// TestAlertingChartTurnsRed pins the behaviour asked of a breached threshold:
// the whole series is recoloured, and the columns above the line are brighter
// still so it is clear when the breach started.
func TestAlertingChartTurnsRed(t *testing.T) {
	// Test output is not a terminal, so lipgloss would strip every escape
	// sequence and the assertions below would be vacuous.
	defer withColor()()

	const redFg = "\x1b[38;5;203m"    // colAlert
	const dimRedFg = "\x1b[38;5;167m" // colAlertBg

	p := panel{
		chart:     config.MetricChart(config.CPU),
		title:     config.Titles[config.CPU],
		series:    metrics.NewSeries(20),
		threshold: 80,
		hasThresh: true,
		rowH:      infoHeight,
		infoW:     infoWidth,
		chartW:    20,
		chartH:    infoHeight - 1,
	}
	for i := 0; i < 10; i++ {
		p.series.Append(metrics.Sample{Value: 20, OK: true})
	}
	calm := p.render()
	if strings.Contains(calm, redFg) || strings.Contains(calm, dimRedFg) {
		t.Error("a panel below its threshold should not render any red")
	}

	p.series.Append(metrics.Sample{Value: 95, OK: true}) // breach
	hot := p.render()
	if !strings.Contains(hot, redFg) {
		t.Error("the breaching column should be bright red")
	}
	if !strings.Contains(hot, dimRedFg) {
		t.Error("the rest of an alerting chart should be reddened too")
	}
	if !strings.Contains(hot, "CPU") {
		t.Error("panel lost its title")
	}
}

// TestThresholdOffScaleIsNotDrawn covers the axis rule that keeps a high
// threshold from flattening the data it is meant to watch.
func TestThresholdOffScaleIsNotDrawn(t *testing.T) {
	s := metrics.NewSeries(20)
	for i := 0; i < 20; i++ {
		s.Append(metrics.Sample{Value: 100, OK: true})
	}
	// Threshold far above anything observed: no guide line, and the data still
	// uses the full height.
	ch := chart{samples: s.Tail(20), width: 20, height: 4, max: 120, threshold: 10000}
	if got := ch.thresholdRow(); got != -1 {
		t.Errorf("thresholdRow = %d, want -1 for an off-scale threshold", got)
	}
	rows := ch.render()
	if strings.TrimSpace(rows[0]) == "" {
		t.Error("data should reach the top row when the axis tracks the data")
	}
}

// withColor forces a colour profile for the duration of a test and returns the
// cleanup that restores it.
func withColor() func() {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	return func() { lipgloss.SetColorProfile(prev) }
}

func TestDumpHelp(t *testing.T) {
	m := newSized(t, 132, 48)
	m.input.SetValue("/help")
	m.submit()
	out := m.View()
	if !strings.Contains(out, "COMMANDS") {
		t.Error("help did not render")
	}
	t.Log("\n" + out)
}

func TestWrapTokens(t *testing.T) {
	const detail = "8 cores · load 2.31 1.98 1.75"

	// Two lines: break at the separator, keeping whole values intact.
	got := wrapTokens(detail, " · ", 25, 2)
	want := []string{"8 cores", "load 2.31 1.98 1.75"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("two lines = %q, want %q", got, want)
	}

	// One line: a plain truncation keeps more information than dropping the
	// tail token would.
	got = wrapTokens(detail, " · ", 25, 1)
	if len(got) != 1 || !strings.HasPrefix(got[0], "8 cores · load 2.3") {
		t.Errorf("one line = %q, want a truncation of the whole string", got)
	}
	if w := width(got[0]); w > 25 {
		t.Errorf("one line is %d cells wide, want <= 25", w)
	}

	// Content that fits is returned untouched.
	if got := wrapTokens("204 · 0B", " · ", 40, 2); len(got) != 1 || got[0] != "204 · 0B" {
		t.Errorf("short content = %q", got)
	}
	// A single token wider than the line is truncated, not dropped.
	if got := wrapTokens("aaaaaaaaaaaaaaaaaaaa", " · ", 6, 1); len(got) != 1 || width(got[0]) > 6 {
		t.Errorf("long token = %q", got)
	}
	if got := wrapTokens("x", " · ", 0, 1); got != nil {
		t.Errorf("zero width = %q, want nil", got)
	}
}

func TestBelowMinimumSizeSaysSo(t *testing.T) {
	m := newSized(t, 30, 10)
	out := m.View()
	if !strings.Contains(out, "too small") || !strings.Contains(out, "/show") {
		t.Errorf("tiny terminal should explain itself and the way out, got %q", out)
	}
}

func TestDumpViewTall(t *testing.T) {
	m := newSized(t, 132, 60)
	seed(m, 400)
	snap := m.cfg.Snapshot()
	l := m.resolveLayout(130, 60-1-2-3, snap.SectionsFor(m.screen), snap.VisibleCount(m.screen))
	if l.rowH != infoHeight {
		t.Errorf("rowH = %d at 60 lines, want the full %d", l.rowH, infoHeight)
	}
	t.Log("\n" + m.View())
}

func TestDumpPicker(t *testing.T) {
	m := newSized(t, 132, 44)
	seed(m, 400)
	m.input.SetValue("/show")
	m.submit()
	if !m.picker {
		t.Fatal("/show did not open the picker")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	t.Log("\n" + m.View())
}

// TestPickerFitsShortTerminal pins the rule that every metric stays selectable
// however short the terminal is: a picker that hides the row you came to unhide
// would be a trap.
func TestPickerFitsShortTerminal(t *testing.T) {
	for _, h := range []int{16, 20, 26, 44} {
		m := newSized(t, 60, h)
		m.picker = true
		out := m.View()
		lines := strings.Split(out, "\n")
		if len(lines) > h {
			t.Errorf("h=%d: picker view is %d lines", h, len(lines))
		}
		for _, k := range config.Order {
			if !strings.Contains(out, config.Titles[k]) {
				t.Errorf("h=%d: %s missing from the picker", h, config.Titles[k])
			}
		}
	}
}

func TestSectionsCoverEveryMetric(t *testing.T) {
	snap := config.Default().Snapshot()
	seen := map[config.ChartID]bool{}
	for _, screen := range config.Screens {
		for _, sec := range snap.AllSections(screen) {
			for _, c := range sec.Charts {
				if seen[c] {
					t.Errorf("%s listed in more than one section", c)
				}
				seen[c] = true
			}
		}
	}
	if len(seen) != len(config.Order) {
		t.Errorf("sections cover %d charts, Order has %d", len(seen), len(config.Order))
	}
	for _, m := range config.Order {
		if !seen[config.MetricChart(m)] {
			t.Errorf("%s has no section", m)
		}
	}
}

func TestPromptLooksInactiveDuringPicker(t *testing.T) {
	m := newSized(t, 132, 44)
	m.input.SetValue("/interval 2s")
	m.picker = true
	out := m.View()
	if strings.Contains(out, "/interval 2s") {
		t.Error("the prompt should not show a live value while the modal picker is open")
	}
	if !strings.Contains(out, "esc to return to the prompt") {
		t.Error("the prompt should say why it is inactive")
	}
	// Closing the picker hands the keyboard back with the input intact.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.picker {
		t.Fatal("esc should close the picker")
	}
	if !strings.Contains(m.View(), "/interval 2s") {
		t.Error("the prompt value should survive the picker")
	}
}

// seedServices configures a few services and fills their charts.
func seedServices(m *Model, n int) {
	names := []string{"nginx", "postgres", "redis-server", "node"}
	base := map[string]float64{"nginx": 12, "postgres": 34, "redis-server": 4, "node": 62}
	for _, name := range names {
		m.cfg.Update(func(s *config.Settings) {
			s.AddService(config.Service{Name: name, Match: name})
		})
	}
	m.syncSeries(m.cfg.Snapshot())

	now := time.Now()
	for i := 0; i < n; i++ {
		wave := 1 + 0.5*math.Sin(float64(i)/7)
		for _, name := range names {
			c := config.ServiceChart(name)
			ok := true
			if name == "redis-server" && i > n-6 {
				ok = false // simulate the process going away
			}
			m.series[c].Append(metrics.Sample{
				At: now.Add(time.Duration(i-n) * time.Second), Value: base[name] * wave, OK: ok,
			})
		}
	}
	m.detail[config.ServiceChart("nginx")] = "5 pid · 0.96 cores · 148M"
	m.detail[config.ServiceChart("postgres")] = "12 pid · 2.72 cores · 1.1G"
	m.detail[config.ServiceChart("node")] = "3 pid · 4.96 cores · 892M"
	m.errmsg[config.ServiceChart("redis-server")] = "no process matching \"redis-server\""
}

func TestDumpServicesScreen(t *testing.T) {
	m := newSized(t, 132, 40)
	seed(m, 400)
	seedServices(m, 300)
	m.screen = config.ScreenServices
	t.Log("\n" + m.View())
}

func TestDumpDiscovery(t *testing.T) {
	m := newSized(t, 132, 40)
	seed(m, 400)
	m.Update(discoverMsg{candidates: []metrics.Candidate{
		{Name: "postgres", PIDs: 12, MemBytes: 1_150_000_000, Ports: []uint32{5432}, Listening: true, CPUPercent: 34},
		{Name: "nginx", PIDs: 5, MemBytes: 148_000_000, Ports: []uint32{80, 443}, Listening: true, CPUPercent: 12},
		{Name: "node", PIDs: 3, MemBytes: 892_000_000, Ports: []uint32{3000, 9229}, Listening: true, CPUPercent: 62},
		{Name: "Docker", PIDs: 8, MemBytes: 2_100_000_000, Listening: false, CPUPercent: 9},
		{Name: "Code Helper (Renderer)", PIDs: 6, MemBytes: 1_900_000_000, Listening: false, CPUPercent: 7},
		{Name: "mds_stores", PIDs: 1, MemBytes: 90_000_000, Listening: false, CPUPercent: 3},
	}})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	t.Log("\n" + m.View())
}

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"/service add nginx", []string{"/service", "add", "nginx"}},
		{"/service add 'My Worker'", []string{"/service", "add", "My Worker"}},
		{`/service add api "java -jar api.jar"`, []string{"/service", "add", "api", "java -jar api.jar"}},
		{"  /show   cpu  ", []string{"/show", "cpu"}},
		{"", nil},
		// An unterminated quote is what a half-typed argument looks like.
		{"/service add 'My Wor", []string{"/service", "add", "My Wor"}},
		{`/host ""`, []string{"/host", ""}},
	}
	for _, c := range cases {
		got := splitArgs(c.in)
		if strings.Join(got, "|") != strings.Join(c.want, "|") || len(got) != len(c.want) {
			t.Errorf("splitArgs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCompletionKeepsQuotedArgument(t *testing.T) {
	m := newSized(t, 140, 60)
	// Completing a later argument must not rewrite an earlier quoted one.
	m.input.SetValue(`/service add "My Worker" `)
	m.refreshSuggestions()
	m.input.SetValue(`/service add "My Worker" ngin`)
	m.refreshSuggestions()
	for _, s := range m.suggestions {
		if !strings.Contains(s.replace, `"My Worker"`) {
			t.Errorf("completion dropped the quoted argument: %q", s.replace)
		}
	}
}

// TestOverlayScrolls covers the panels that outgrow a short terminal: they must
// scroll rather than silently drop the line you opened them to read.
func TestOverlayScrolls(t *testing.T) {
	m := newSized(t, 132, 30)
	m.input.SetValue("/help")
	m.submit()

	first := m.View()
	if !strings.Contains(first, "to scroll") {
		t.Fatal("a panel taller than the terminal should offer scrolling")
	}
	if !strings.Contains(first, "/help") {
		t.Error("the top of the list should be visible first")
	}

	// End jumps to the bottom, which holds the chart legend.
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	last := m.View()
	if strings.Contains(last, "COMMANDS") {
		t.Error("End did not scroll away from the top")
	}
	if !strings.Contains(last, "no data yet") {
		t.Error("the bottom of the list should be reachable")
	}

	// Scrolling never runs off either end.
	for i := 0; i < 200; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.View()
	for i := 0; i < 200; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	if m.overlayOffset != 0 {
		t.Errorf("offset = %d after scrolling up past the start", m.overlayOffset)
	}
	if !strings.Contains(m.View(), "COMMANDS") {
		t.Error("scrolling back up should show the top again")
	}

	// Scroll keys do not dismiss; anything else does.
	if m.overlay == nil {
		t.Fatal("scrolling dismissed the panel")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m.overlay != nil {
		t.Error("a normal key should dismiss the panel")
	}
	if m.overlayOffset != 0 {
		t.Error("dismissing should reset the scroll position")
	}
}

// TestOverlayColumnsNeverCollide guards the key/value columns of the panels
// against a long service name running into its value.
func TestOverlayColumnsNeverCollide(t *testing.T) {
	m := newSized(t, 132, 48)
	m.cfg.Update(func(s *config.Settings) {
		s.AddService(config.Service{Name: "a-very-long-service-name-here", Match: "x"})
		s.AddService(config.Service{Name: "postgres", Match: "postgres"})
	})
	snap := m.cfg.Snapshot()

	for _, lines := range [][]string{
		thresholdsOverlay(snap),
		configOverlay(snap, false, "/tmp/status.toml"),
		servicesOverlay(snap),
	} {
		for _, l := range lines {
			// A digit or a % landing immediately after a name means the key
			// column was too narrow.
			if strings.Contains(l, "service:postgres5") || strings.Contains(l, "-here5") {
				t.Errorf("key and value collided: %q", l)
			}
		}
	}

	// And the threshold list must actually contain both services.
	joined := strings.Join(thresholdsOverlay(snap), "\n")
	for _, want := range []string{"service:postgres", "service:a-very-long-service-name-here"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%s missing from the threshold list", want)
		}
	}
}

// TestOverlayDismissalDoesNotLeakKeys covers the wrinkle that closing a panel
// with an ordinary keystroke used to type that character onto the prompt, so the
// next command was submitted with junk in front of it.
func TestOverlayDismissalDoesNotLeakKeys(t *testing.T) {
	open := func(m *Model) {
		m.input.SetValue("/config")
		m.submit()
		if m.overlay == nil {
			t.Fatal("/config did not open a panel")
		}
	}

	m := newSized(t, 132, 48)
	open(m)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	if m.overlay != nil {
		t.Error("an ordinary key should dismiss the panel")
	}
	if got := m.input.Value(); got != "" {
		t.Errorf("dismissing left %q on the prompt", got)
	}

	// "/" is the deliberate exception: read, then start typing straight away.
	open(m)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if m.overlay != nil {
		t.Error("/ should dismiss the panel")
	}
	if got := m.input.Value(); got != "/" {
		t.Errorf("prompt = %q, want the slash to have gone through", got)
	}
	if len(m.suggestions) == 0 {
		t.Error("/ should open the completion popup")
	}

	// Control keys keep working with a panel open.
	m.input.SetValue("")
	open(m)
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.overlay != nil {
		t.Error("shift+tab should dismiss the panel")
	}
	if m.screen != config.ScreenServices {
		t.Error("shift+tab should still switch screens with a panel open")
	}
}
