package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"status/internal/config"
	"status/internal/metrics"
)

// drain runs a command tree synchronously and feeds every resulting message
// back into the model, skipping the timer messages that would otherwise block
// for a whole interval.
func drain(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			drain(t, m, c)
		}
		return
	}
	switch msg.(type) {
	case nil, sysTickMsg, checkTickMsg:
		return
	}
	_, _ = m.Update(msg)
}

// TestLiveCollection exercises the real collectors and probes end to end.
func TestLiveCollection(t *testing.T) {
	if testing.Short() {
		t.Skip("needs network access")
	}
	m := newSized(t, 140, 44)

	// Two system samples: the first primes the counters, the second yields rates.
	drain(t, m, m.collectSystem())
	drain(t, m, m.collectSystem())
	drain(t, m, m.runChecks())

	for _, k := range config.Order {
		s := m.series[config.MetricChart(k)]
		if s.Len() == 0 {
			t.Errorf("%s: no samples collected", k)
			continue
		}
		last, _ := s.Last()
		t.Logf("%-8s len=%d ok=%v value=%v detail=%q err=%q",
			k, s.Len(), last.OK, last.Value, m.detail[config.MetricChart(k)], m.errmsg[config.MetricChart(k)])
	}

	for _, k := range config.Probes {
		if m.inflight[k] {
			t.Errorf("%s: still marked in flight after its result landed", k)
		}
	}

	out := m.View()
	if strings.Contains(out, "starting…") {
		t.Error("view never left its startup state")
	}
}

// TestCommands walks the commands a user is most likely to type.
func TestCommands(t *testing.T) {
	m := newSized(t, 140, 44)

	// The returned commands are discarded: several of them kick off a real
	// probe run, which this test does not need and should not wait for.
	run := func(input string) string {
		m.input.SetValue(input)
		m.submit()
		if m.statusErr {
			t.Errorf("%q failed: %s", input, m.status)
		}
		return m.status
	}
	runFail := func(input string) {
		m.input.SetValue(input)
		m.submit()
		if !m.statusErr {
			t.Errorf("%q should have been rejected, got %q", input, m.status)
		}
	}

	t.Log(run("/interval 30s"))
	if got := m.cfg.Snapshot().Interval.String(); got != "30s" {
		t.Errorf("interval = %s, want 30s", got)
	}
	t.Log(run("/sysinterval 500ms"))
	t.Log(run("/timeout all 3s"))
	for _, k := range config.Probes {
		if got := m.cfg.Snapshot().Timeout(k).String(); got != "3s" {
			t.Errorf("timeout[%s] = %s, want 3s", k, got)
		}
	}
	t.Log(run("/timeout dns 750ms"))
	t.Log(run("/threshold cpu 50"))
	t.Log(run("/threshold net 250MB/s"))
	if got, _ := m.cfg.Snapshot().Threshold(config.MetricChart(config.Net)); got != 250*1024*1024 {
		t.Errorf("threshold[net] = %v, want %v", got, 250*1024*1024)
	}
	t.Log(run("/threshold ttfb 1.5s"))
	if got, _ := m.cfg.Snapshot().Threshold(config.MetricChart(config.TTFB)); got != 1500 {
		t.Errorf("threshold[ttfb] = %v, want 1500", got)
	}
	t.Log(run("/threshold ping 0"))
	if _, on := m.cfg.Snapshot().Threshold(config.MetricChart(config.Ping)); on {
		t.Error("threshold[ping] should be disabled")
	}
	t.Log(run("/pingmode tcp"))
	t.Log(run("/insecure on"))
	if !m.cfg.Snapshot().InsecureTLS {
		t.Error("insecure should be on")
	}
	t.Log(run("/insecure off"))
	t.Log(run("/history 120"))
	t.Log(run("/pause"))
	if !m.paused {
		t.Error("should be paused")
	}
	t.Log(run("/resume"))
	t.Log(run("/host example.com/health"))
	if got := m.cfg.Snapshot().URL(); got != "https://example.com/health" {
		t.Errorf("url = %s", got)
	}
	t.Log(run("/host http://localhost:8080"))
	if got := m.cfg.Snapshot().URL(); got != "http://localhost:8080/" {
		t.Errorf("url = %s", got)
	}
	t.Log(run("/reset"))
	if got := m.cfg.Snapshot().Host; got != "status.azion.app" {
		t.Errorf("host after reset = %s", got)
	}

	runFail("/interval banana")
	runFail("/threshold cpu 500")
	runFail("/threshold nope 10")
	runFail("/timeout cpu 1s")
	runFail("/nosuchcommand")
	runFail("interval 5s")
	runFail("/host ftp://example.com")
	runFail("/history 2")
}

// TestCompletion covers the prompt behaviour: filtering, Tab, and Enter.
func TestCompletion(t *testing.T) {
	m := newSized(t, 140, 44)

	m.input.SetValue("/thr")
	m.refreshSuggestions()
	if len(m.suggestions) != 2 {
		t.Fatalf("want 2 matches for /thr, got %d: %+v", len(m.suggestions), m.suggestions)
	}

	// Tab completes to the highlighted entry.
	m.acceptSuggestion(false)
	if got := m.input.Value(); got != "/threshold " {
		t.Errorf("after tab: %q", got)
	}

	// Argument completion for the metric name.
	m.input.SetValue("/threshold di")
	m.refreshSuggestions()
	got := make([]string, len(m.suggestions))
	for i, s := range m.suggestions {
		got[i] = s.display
	}
	if strings.Join(got, ",") != "disk,diskio" {
		t.Errorf("metric completions = %v", got)
	}
	m.acceptSuggestion(false)
	if v := m.input.Value(); v != "/threshold disk " {
		t.Errorf("after arg completion: %q", v)
	}

	// Enter on an argument-less command completes and runs it in one press.
	m.input.SetValue("/paus")
	m.refreshSuggestions()
	m.acceptSuggestion(true)
	if !m.paused {
		t.Error("enter on /paus should have completed and run /pause")
	}
	if m.input.Value() != "" {
		t.Errorf("prompt not cleared after run: %q", m.input.Value())
	}
}

// TestHistoryNavigation checks the up/down recall of previous commands.
func TestHistoryNavigation(t *testing.T) {
	m := newSized(t, 140, 44)
	for _, c := range []string{"/interval 10s", "/pause", "/resume"} {
		m.input.SetValue(c)
		m.submit()
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.input.Value(); got != "/resume" {
		t.Errorf("first up = %q, want /resume", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.input.Value(); got != "/interval 10s" {
		t.Errorf("third up = %q, want /interval 10s", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.input.Value(); got != "/pause" {
		t.Errorf("down = %q, want /pause", got)
	}
}

// TestPauseStopsScheduling makes sure a paused dashboard arms no timers.
func TestPauseStopsScheduling(t *testing.T) {
	m := newSized(t, 140, 44)
	m.paused = true
	if c := m.scheduleChecks(); c != nil {
		t.Error("scheduleChecks armed a timer while paused")
	}
	if c := m.scheduleSystem(); c != nil {
		t.Error("scheduleSystem armed a timer while paused")
	}
	if c := m.runChecks(); c != nil {
		t.Error("runChecks ran while paused")
	}
}

// TestStaleTickIgnored covers the epoch guard that drops superseded timers.
func TestStaleTickIgnored(t *testing.T) {
	m := newSized(t, 140, 44)
	m.scheduleChecks() // epoch 1
	stale := m.checkEpoch
	m.scheduleChecks() // epoch 2 supersedes it
	_, cmd := m.Update(checkTickMsg{epoch: stale})
	if cmd != nil {
		t.Error("a superseded tick should produce no work")
	}
}

// TestSystemCollectorNotRunConcurrently guards the invariant that makes the
// rate calculations correct: only one Collect may be in flight at a time.
func TestSystemCollectorNotRunConcurrently(t *testing.T) {
	m := newSized(t, 140, 44)
	m.Init()
	if !m.sysInflight {
		t.Fatal("Init should mark the first system sample as in flight")
	}
	// A tick arriving while the sample is still running must only re-arm.
	_, cmd := m.Update(sysTickMsg{epoch: m.sysEpoch})
	if cmd == nil {
		t.Fatal("a tick during collection should still re-arm the timer")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("re-arm produced no message")
	} else if _, isTick := msg.(sysTickMsg); !isTick {
		t.Errorf("tick during collection started work instead of re-arming: %T", msg)
	}

	// Once the result lands, the next tick is free to collect again.
	m.Update(sysResultMsg{})
	if m.sysInflight {
		t.Error("result did not clear the in-flight flag")
	}
}

// TestProbeInflightGuard checks a slow check is not started twice.
func TestProbeInflightGuard(t *testing.T) {
	m := newSized(t, 140, 44)
	if c := m.runChecks(); c == nil {
		t.Fatal("first run produced no work")
	}
	for _, k := range config.Probes {
		if !m.inflight[k] {
			t.Errorf("%s not marked in flight", k)
		}
	}
	if c := m.runChecks(); c != nil {
		t.Error("a second run started while every check was still in flight")
	}
}

// TestLoopKeepsSampling runs the model under the real Bubble Tea runtime to
// prove both sampling loops keep re-arming. A scheduling regression — a dropped
// re-arm, an epoch that never matches — leaves the dashboard frozen on its first
// sample, which no unit test on Update alone would catch.
func TestLoopKeepsSampling(t *testing.T) {
	if testing.Short() {
		t.Skip("needs network access")
	}
	const (
		sysEvery   = 200 * time.Millisecond
		checkEvery = 500 * time.Millisecond
		runFor     = 2500 * time.Millisecond
	)

	cfg := config.Default()
	cfg.Update(func(s *config.Settings) {
		s.SysInterval = sysEvery
		s.Interval = checkEvery
	})
	m := New(cfg)
	m.w, m.h, m.ready = 140, 44, true

	p := tea.NewProgram(m, tea.WithoutRenderer(), tea.WithInput(nil))
	go func() {
		time.Sleep(runFor)
		p.Quit()
	}()
	if _, err := p.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Allow generous slack: probes take real time and the loops deliberately
	// skip a tick rather than overlap a slow check.
	wantSys := int(runFor/sysEvery) / 2
	wantCheck := int(runFor/checkEvery) / 2
	for _, k := range config.Order {
		got := m.series[config.MetricChart(k)].Len()
		want := wantSys
		if isProbe(k) {
			want = wantCheck
		}
		t.Logf("%-8s samples=%d (want >= %d)", k, got, want)
		if got < want {
			t.Errorf("%s: %d samples in %s, the loop stalled", k, got, runFor)
		}
	}
}

// TestShowCommand covers every form of /show.
func TestShowCommand(t *testing.T) {
	m := newSized(t, 140, 60)
	snap := func() config.Settings { return m.cfg.Snapshot() }

	run := func(input string) string {
		m.input.SetValue(input)
		m.submit()
		if m.statusErr {
			t.Errorf("%q failed: %s", input, m.status)
		}
		return m.status
	}

	if snap().VisibleCount(config.ScreenMain) != len(config.Order) {
		t.Fatal("everything should start visible")
	}

	// A bare /show opens the picker rather than changing anything.
	run("/show")
	if !m.picker {
		t.Error("/show should open the picker")
	}
	if snap().VisibleCount(config.ScreenMain) != len(config.Order) {
		t.Error("/show must not hide anything by itself")
	}
	m.picker = false

	// A metric name toggles that one chart.
	t.Log(run("/show diskio"))
	if snap().IsShown(config.MetricChart(config.DiskIO)) {
		t.Error("diskio should be hidden")
	}
	t.Log(run("/show diskio"))
	if !snap().IsShown(config.MetricChart(config.DiskIO)) {
		t.Error("diskio should be visible again")
	}

	// A section name hides the whole group, then brings it back.
	t.Log(run("/show machine"))
	for _, k := range config.MachineMetrics {
		if snap().IsShown(config.MetricChart(k)) {
			t.Errorf("%s should be hidden with the machine section", k)
		}
	}
	if !snap().IsShown(config.MetricChart(config.Ping)) {
		t.Error("the network section should be untouched")
	}
	t.Log(run("/show machine"))
	if snap().VisibleCount(config.ScreenMain) != len(config.Order) {
		t.Error("machine section should be back")
	}

	// A partly hidden section is restored, not hidden further.
	run("/show cpu")
	t.Log(run("/show machine"))
	if !snap().IsShown(config.MetricChart(config.CPU)) {
		t.Error("a section with something hidden should be restored")
	}

	t.Log(run("/show none"))
	if snap().VisibleCount(config.ScreenMain) != 0 {
		t.Error("/show none should hide everything")
	}
	if out := m.View(); !strings.Contains(out, "every chart on this screen is hidden") {
		t.Errorf("an empty dashboard should say so, got %q", out)
	}
	t.Log(run("/show all"))
	if snap().VisibleCount(config.ScreenMain) != len(config.Order) {
		t.Error("/show all should restore everything")
	}

	m.input.SetValue("/show nonsense")
	m.submit()
	if !m.statusErr {
		t.Error("/show nonsense should be rejected")
	}
}

// TestHiddenChartLeavesLayoutAndKeepsAlerting checks the two things hiding must
// do: free the row, and not silence the metric.
func TestHiddenChartLeavesLayoutAndKeepsAlerting(t *testing.T) {
	m := newSized(t, 140, 60)
	m.series[config.MetricChart(config.CPU)].Append(metrics.Sample{Value: 99, OK: true})
	m.input.SetValue("/threshold cpu 1")
	m.submit()

	if vis, hid := m.alertCount(m.cfg.Snapshot()); vis != 1 || hid != 0 {
		t.Errorf("alerts = %d visible / %d hidden, want 1/0", vis, hid)
	}

	m.input.SetValue("/show cpu")
	m.submit()

	snap := m.cfg.Snapshot()
	if vis, hid := m.alertCount(snap); vis != 0 || hid != 1 {
		t.Errorf("after hiding: alerts = %d visible / %d hidden, want 0/1", vis, hid)
	}
	bar := m.renderTopBar(138, m.resolveLayout(138, 40, snap.SectionsFor(m.screen), snap.VisibleCount(m.screen)))
	if !strings.Contains(bar, "UNSEEN") {
		t.Errorf("top bar should flag the unseen alert, got %q", bar)
	}

	// The feedback line still names the chart that was just hidden, so assert
	// against the grid itself.
	out := m.renderGrid(m.resolveLayout(138, 40, snap.SectionsFor(m.screen), snap.VisibleCount(m.screen)))
	if strings.Contains(out, config.Titles[config.CPU]) {
		t.Error("a hidden chart should not be drawn")
	}
	if !strings.Contains(out, config.Titles[config.Mem]) {
		t.Error("the rest of the section should still be drawn")
	}
	// Hidden metrics keep collecting, so unhiding restores history.
	before := m.series[config.MetricChart(config.CPU)].Len()
	drain(t, m, m.collectSystem())
	if m.series[config.MetricChart(config.CPU)].Len() <= before {
		t.Error("a hidden metric should keep collecting")
	}
}

// TestPickerKeys drives the /show list from the keyboard.
func TestPickerKeys(t *testing.T) {
	m := newSized(t, 140, 60)
	m.picker = true

	key := func(k tea.KeyMsg) { m.Update(k) }
	runes := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

	// Down twice then toggle hits the third metric in display order.
	key(tea.KeyMsg{Type: tea.KeyDown})
	key(tea.KeyMsg{Type: tea.KeyDown})
	key(tea.KeyMsg{Type: tea.KeySpace})
	if all := m.cfg.Snapshot().AllCharts(); m.cfg.Snapshot().IsShown(all[2]) {
		t.Errorf("space should have hidden %s", all[2])
	}

	// Wrapping: up from the top lands on the last entry.
	key(tea.KeyMsg{Type: tea.KeyUp})
	key(tea.KeyMsg{Type: tea.KeyUp})
	key(tea.KeyMsg{Type: tea.KeyUp})
	if m.pickerIdx != len(config.Order)-1 {
		t.Errorf("pickerIdx = %d, want %d after wrapping", m.pickerIdx, len(config.Order)-1)
	}

	key(runes("n"))
	if m.cfg.Snapshot().VisibleCount(config.ScreenMain) != 0 {
		t.Error("n should hide everything")
	}
	key(runes("a"))
	if m.cfg.Snapshot().VisibleCount(config.ScreenMain) != len(config.Order) {
		t.Error("a should show everything")
	}

	// Keys must not leak into the prompt behind the picker.
	key(runes("q"))
	if m.picker {
		t.Error("q should close the picker")
	}
	if m.input.Value() != "" {
		t.Errorf("picker keys leaked into the prompt: %q", m.input.Value())
	}
}

// TestServiceLifecycle covers adding, charting and removing a service by hand.
func TestServiceLifecycle(t *testing.T) {
	m := newSized(t, 140, 60)
	snap := func() config.Settings { return m.cfg.Snapshot() }

	run := func(input string) string {
		m.input.SetValue(input)
		m.submit()
		if m.statusErr {
			t.Errorf("%q failed: %s", input, m.status)
		}
		return m.status
	}

	if got := snap().VisibleCount(config.ScreenServices); got != 0 {
		t.Fatalf("services screen starts with %d charts, want 0", got)
	}
	// An empty services screen must explain itself, not look broken.
	m.screen = config.ScreenServices
	if out := m.View(); !strings.Contains(out, "/discover") {
		t.Errorf("empty services screen should point at /discover, got %q", out)
	}

	t.Log(run("/service add nginx"))
	id := config.ServiceChart("nginx")
	if _, ok := snap().Service("nginx"); !ok {
		t.Fatal("nginx not registered")
	}
	if m.series[id] == nil {
		t.Error("adding a service should create its series")
	}
	if th, ok := snap().Threshold(id); !ok || th != snap().ServiceThreshold {
		t.Errorf("threshold = %v (set %v), want the %v default", th, ok, snap().ServiceThreshold)
	}
	if got := snap().VisibleCount(config.ScreenServices); got != 1 {
		t.Errorf("services screen shows %d charts, want 1", got)
	}
	if snap().ScreenOf(id) != config.ScreenServices {
		t.Error("a service belongs on the services screen")
	}

	// A match containing a space implies looking at the command line.
	t.Log(run("/service add api java -jar api.jar"))
	svc, _ := snap().Service("api")
	if svc.Match != "java -jar api.jar" || !svc.Cmdline {
		t.Errorf("service = %+v, want the cmdline match", svc)
	}

	// A quoted name survives tokenisation and is normalised into an identifier.
	t.Log(run("/service add 'My Worker'"))
	if _, ok := snap().Service("my-worker"); !ok {
		t.Errorf("quoted name not normalised: %+v", snap().Services)
	}

	// Per-service thresholds go through /threshold like any other chart.
	t.Log(run("/threshold nginx 25"))
	if th, _ := snap().Threshold(id); th != 25 {
		t.Errorf("nginx threshold = %v, want 25", th)
	}

	// /show reaches services from the main screen.
	m.screen = config.ScreenMain
	t.Log(run("/show nginx"))
	if snap().IsShown(id) {
		t.Error("nginx should be hidden")
	}
	t.Log(run("/show nginx"))

	t.Log(run("/service rm nginx"))
	if _, ok := snap().Service("nginx"); ok {
		t.Error("nginx still registered")
	}
	if m.series[id] != nil {
		t.Error("removing a service should drop its series")
	}
	if _, ok := snap().Thresholds[id]; ok {
		t.Error("removing a service should drop its threshold")
	}

	m.input.SetValue("/service rm nope")
	m.submit()
	if !m.statusErr {
		t.Error("removing an unknown service should be rejected")
	}
	m.input.SetValue("/service wat")
	m.submit()
	if !m.statusErr {
		t.Error("an unknown subcommand should be rejected")
	}
}

// TestDiscoveryFlow drives the /discover picker with synthetic candidates.
func TestDiscoveryFlow(t *testing.T) {
	m := newSized(t, 140, 60)
	cands := []metrics.Candidate{
		{Name: "postgres", PIDs: 12, Listening: true, Ports: []uint32{5432}},
		{Name: "nginx", PIDs: 5, Listening: true, Ports: []uint32{80}},
		{Name: "Code Helper", PIDs: 6},
	}

	m.Update(discoverMsg{candidates: cands})
	if !m.discovering {
		t.Fatal("results should open the picker")
	}
	// Nothing is committed until Enter.
	if got := len(m.cfg.Snapshot().Services); got != 0 {
		t.Errorf("%d services added before confirming", got)
	}

	// Esc discards the whole selection.
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.discovering || len(m.cfg.Snapshot().Services) != 0 {
		t.Error("esc should cancel without adding anything")
	}

	// "l" ticks everything holding a listening socket.
	m.Update(discoverMsg{candidates: cands})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if len(m.chosen) != 2 {
		t.Errorf("selected %d, want the 2 listening candidates", len(m.chosen))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.discovering {
		t.Error("enter should close the picker")
	}
	snap := m.cfg.Snapshot()
	if len(snap.Services) != 2 {
		t.Fatalf("added %d services, want 2: %+v", len(snap.Services), snap.Services)
	}
	for _, name := range []string{"nginx", "postgres"} {
		if _, ok := snap.Service(name); !ok {
			t.Errorf("%s not added", name)
		}
		if m.series[config.ServiceChart(name)] == nil {
			t.Errorf("%s has no series", name)
		}
	}
	// A name that is not a valid identifier is normalised on the way in.
	m.Update(discoverMsg{candidates: []metrics.Candidate{{Name: "Code Helper (Renderer)"}}})
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if _, ok := m.cfg.Snapshot().Service("code-helper--renderer"); !ok {
		t.Errorf("normalised name missing: %+v", m.cfg.Snapshot().Services)
	}

	// An empty scan says so rather than opening an empty list.
	m.Update(discoverMsg{candidates: nil})
	if m.discovering {
		t.Error("an empty result should not open the picker")
	}
	if !m.statusErr {
		t.Error("an empty result should be reported")
	}
}

// TestScreenSwitchKeepsHistory is the guarantee that switching screens is a
// change of view and nothing else: both screens keep collecting throughout.
func TestScreenSwitchKeepsHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("needs process enumeration")
	}
	m := newSized(t, 140, 60)
	m.input.SetValue("/service add " + selfProcessName(t))
	m.submit()
	svc := config.ServiceChart(config.NormaliseServiceName(selfProcessName(t)))

	// Warm both collectors up, then take a real sample of each.
	drain(t, m, m.collectSystem())
	drain(t, m, m.collectServices())
	drain(t, m, m.collectSystem())
	drain(t, m, m.collectServices())

	cpu := config.MetricChart(config.CPU)
	beforeCPU, beforeSvc := m.series[cpu].Len(), m.series[svc].Len()
	if beforeCPU == 0 || beforeSvc == 0 {
		t.Fatalf("nothing collected: cpu=%d svc=%d", beforeCPU, beforeSvc)
	}

	// Shift+Tab cycles, and back again.
	if m.screen != config.ScreenMain {
		t.Fatal("should start on the main screen")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.screen != config.ScreenServices {
		t.Fatalf("shift+tab left us on %s", m.screen)
	}
	if got := m.series[cpu].Len(); got != beforeCPU {
		t.Errorf("cpu history changed on screen switch: %d -> %d", beforeCPU, got)
	}

	// The screen we are not looking at keeps collecting.
	drain(t, m, m.collectSystem())
	drain(t, m, m.collectServices())
	if got := m.series[cpu].Len(); got <= beforeCPU {
		t.Error("the main screen stopped collecting while hidden behind the services screen")
	}
	if got := m.series[svc].Len(); got <= beforeSvc {
		t.Error("the services screen stopped collecting")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.screen != config.ScreenMain {
		t.Errorf("shift+tab did not cycle back, on %s", m.screen)
	}
	if out := m.View(); !strings.Contains(out, config.Titles[config.CPU]) {
		t.Error("the main screen did not come back")
	}

	// /screen addresses a screen directly.
	m.input.SetValue("/screen services")
	m.submit()
	if m.screen != config.ScreenServices {
		t.Error("/screen services did not switch")
	}
	m.input.SetValue("/screen nope")
	m.submit()
	if !m.statusErr {
		t.Error("/screen nope should be rejected")
	}
}

// selfProcessName is a process name guaranteed to be running right now.
func selfProcessName(t *testing.T) string {
	t.Helper()
	name, err := metrics.SelfName()
	if err != nil {
		t.Skipf("cannot read this process's name: %v", err)
	}
	return name
}

// TestServiceCPUIsCollected samples a real process group end to end.
func TestServiceCPUIsCollected(t *testing.T) {
	if testing.Short() {
		t.Skip("needs process enumeration")
	}
	name := selfProcessName(t)
	coll := metrics.NewServiceCollector(4)
	specs := []metrics.ServiceSpec{{Name: "self", Match: name}}

	first := coll.Collect(context.Background(), specs)
	if len(first) != 1 {
		t.Fatalf("got %d samples", len(first))
	}
	if !first[0].Warmup {
		t.Error("the first sample has no counter to diff against and must say so")
	}
	if first[0].PIDs == 0 {
		t.Fatalf("no process matched %q", name)
	}

	busyUntil := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(busyUntil) { //nolint:revive // deliberately burning CPU
	}

	second := coll.Collect(context.Background(), specs)
	if second[0].Warmup {
		t.Error("the second sample should have a rate")
	}
	if second[0].Err != nil {
		t.Errorf("err = %v", second[0].Err)
	}
	t.Logf("cpu=%.2f%% cores=%.3f pids=%d mem=%s",
		second[0].CPUPercent, second[0].Cores, second[0].PIDs,
		metrics.FormatBytes(float64(second[0].MemBytes)))
	if second[0].CPUPercent <= 0 {
		t.Error("burning CPU for 150ms should show up as a non-zero share")
	}
	if second[0].CPUPercent > 100 {
		t.Errorf("cpu share %v exceeds the whole machine", second[0].CPUPercent)
	}

	// A spec matching nothing is an error, not a silent zero.
	miss := coll.Collect(context.Background(), []metrics.ServiceSpec{{Name: "x", Match: "definitely-not-running-xyzzy"}})
	if miss[0].Err == nil {
		t.Error("a spec matching no process should report an error")
	}
}

// TestDiscoverFindsThisProcess checks discovery against the real machine.
func TestDiscoverFindsThisProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("needs process enumeration")
	}
	name := selfProcessName(t)
	cands, err := metrics.Discover(context.Background(), 8, "")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(cands) == 0 {
		t.Fatal("no candidates on a running machine")
	}
	found := false
	for _, c := range cands {
		if c.Name == name {
			found = true
		}
	}
	if !found {
		t.Errorf("discovery missed this very process (%q)", name)
	}
	// Listening processes must sort ahead of the rest.
	sawQuiet := false
	for _, c := range cands {
		if !c.Listening {
			sawQuiet = true
			continue
		}
		if sawQuiet {
			t.Error("a listening candidate sorted after a non-listening one")
			break
		}
	}
	t.Logf("%d candidates, first: %+v", len(cands), cands[0])

	// A filter narrows the list without erroring.
	filtered, err := metrics.Discover(context.Background(), 8, name)
	if err != nil {
		t.Fatalf("filtered Discover: %v", err)
	}
	if len(filtered) == 0 || len(filtered) > len(cands) {
		t.Errorf("filter yielded %d of %d candidates", len(filtered), len(cands))
	}
}
