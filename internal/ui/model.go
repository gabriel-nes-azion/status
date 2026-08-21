package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"status/internal/config"
	"status/internal/metrics"
	"status/internal/probe"
)

// Messages driving the two independent sampling loops.
type (
	sysTickMsg     struct{ epoch int }
	checkTickMsg   struct{ epoch int }
	svcTickMsg     struct{ epoch int }
	sysResultMsg   struct{ snap metrics.SystemSnapshot }
	probeResultMsg struct{ results []probe.Result }
	svcResultMsg   struct{ samples []metrics.ServiceSample }
	discoverMsg    struct {
		candidates []metrics.Candidate
		err        error
	}
)

// suggestion is one entry in the completion popup.
type suggestion struct {
	display string // shown in the list
	desc    string
	replace string // the whole new prompt value when accepted
	runNow  bool   // command takes no arguments, so accepting can run it
}

// Model is the Bubble Tea model for the whole dashboard.
type Model struct {
	cfg     *config.Config
	prober  *probe.Prober
	coll    *metrics.Collector
	svcColl *metrics.ServiceCollector

	series map[config.ChartID]*metrics.Series
	detail map[config.ChartID]string
	errmsg map[config.ChartID]string

	// screen is the page on show. Every chart keeps collecting regardless, so
	// switching screens never loses history.
	screen config.Screen

	// inflight keeps a slow check from being started again on the next tick.
	inflight map[config.Metric]bool
	// sysInflight does the same for the system collector, which is not safe to
	// run concurrently: it derives rates from counters it stores between calls.
	sysInflight bool
	// svcInflight likewise guards the service collector, which walks the whole
	// process table and diffs CPU counters it holds between calls.
	svcInflight bool

	input   textinput.Model
	history []string
	histIdx int // len(history) means "not browsing"

	suggestions []suggestion
	suggIdx     int

	status    string
	statusErr bool

	overlay      []string
	overlayTitle string
	// overlayOffset scrolls a panel taller than the terminal. /help and /config
	// both outgrow a short screen, and truncating them silently hides the very
	// line someone opened the panel to read.
	overlayOffset int

	// picker is the interactive /show list. It needs live state rather than the
	// pre-rendered overlay above, because toggling has to redraw a checkbox.
	picker    bool
	pickerIdx int

	// discovering is the /discover result list: processes proposed for charting,
	// ticked and then confirmed.
	discovering bool
	scanning    bool
	candidates  []metrics.Candidate
	discoverSel selection

	// managing is the /service list: the services already charted, where ticking
	// a row marks it to stop being charted. Kept separate from the picker, which
	// edits visibility rather than configuration.
	managing  bool
	manageSel selection

	paused bool

	// epochs invalidate timers that were superseded by an interval change.
	sysEpoch   int
	checkEpoch int
	svcEpoch   int

	// chartW is the panel width last used, so the header can report the time
	// span currently visible.
	chartW int

	w, h  int
	ready bool
}

// New builds the initial model.
func New(cfg *config.Config) *Model {
	snap := cfg.Snapshot()

	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "type / for commands"
	ti.CharLimit = 240
	ti.Focus()

	coll := metrics.NewCollector()
	m := &Model{
		cfg:      cfg,
		prober:   probe.New(),
		coll:     coll,
		svcColl:  metrics.NewServiceCollector(coll.Cores()),
		series:   make(map[config.ChartID]*metrics.Series, len(config.Order)),
		detail:   make(map[config.ChartID]string, len(config.Order)),
		errmsg:   make(map[config.ChartID]string, len(config.Order)),
		inflight: make(map[config.Metric]bool, len(config.Probes)),
		input:    ti,
		chartW:   60,
	}
	m.discoverSel = newSelection()
	m.manageSel = newSelection()
	m.syncSeries(snap)
	m.histIdx = 0
	return m
}

// syncSeries creates a series for every configured chart and drops the ones
// whose service is gone, so a /service rm does not leak history.
func (m *Model) syncSeries(snap config.Settings) {
	live := make(map[config.ChartID]bool, len(snap.AllCharts()))
	for _, c := range snap.AllCharts() {
		live[c] = true
		if m.series[c] == nil {
			m.series[c] = metrics.NewSeries(snap.History)
		}
	}
	for c := range m.series {
		if !live[c] {
			delete(m.series, c)
			delete(m.detail, c)
			delete(m.errmsg, c)
		}
	}
}

func (m *Model) Init() tea.Cmd {
	m.sysInflight = true
	return tea.Batch(
		textinput.Blink,
		m.collectSystem(),
		m.scheduleSystem(),
		m.runChecks(),
		m.scheduleChecks(),
		m.collectServices(),
		m.scheduleServices(),
	)
}

// --- sampling loops -------------------------------------------------------

// scheduleSystem arms the next system-metrics tick, invalidating any pending one.
func (m *Model) scheduleSystem() tea.Cmd {
	if m.paused {
		return nil
	}
	m.sysEpoch++
	epoch := m.sysEpoch
	return tea.Tick(m.cfg.Snapshot().SysInterval, func(time.Time) tea.Msg {
		return sysTickMsg{epoch: epoch}
	})
}

// scheduleChecks arms the next network-check tick, invalidating any pending one.
func (m *Model) scheduleChecks() tea.Cmd {
	if m.paused {
		return nil
	}
	m.checkEpoch++
	epoch := m.checkEpoch
	return tea.Tick(m.cfg.Snapshot().Interval, func(time.Time) tea.Msg {
		return checkTickMsg{epoch: epoch}
	})
}

// scheduleServices arms the next service sampling tick. Services follow the
// system cadence: they are local resource usage, not a remote check.
func (m *Model) scheduleServices() tea.Cmd {
	if m.paused {
		return nil
	}
	m.svcEpoch++
	epoch := m.svcEpoch
	return tea.Tick(m.cfg.Snapshot().SysInterval, func(time.Time) tea.Msg {
		return svcTickMsg{epoch: epoch}
	})
}

// collectServices samples every configured service in one pass over the process
// table. With no services configured this costs nothing.
func (m *Model) collectServices() tea.Cmd {
	snap := m.cfg.Snapshot()
	if len(snap.Services) == 0 {
		return nil
	}
	specs := make([]metrics.ServiceSpec, 0, len(snap.Services))
	for _, svc := range snap.Services {
		specs = append(specs, metrics.ServiceSpec{Name: svc.Name, Match: svc.Match, Cmdline: svc.Cmdline})
	}
	coll := m.svcColl
	m.svcInflight = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return svcResultMsg{samples: coll.Collect(ctx, specs)}
	}
}

// discover scans the machine for candidate services.
func (m *Model) discover(filter string) tea.Cmd {
	cores := m.coll.Cores()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cands, err := metrics.Discover(ctx, cores, filter)
		return discoverMsg{candidates: cands, err: err}
	}
}

// collectSystem samples every local metric once. Collect mutates the collector's
// previous-counter state, so exactly one of these is ever in flight: the loop
// re-arms only after the result lands.
func (m *Model) collectSystem() tea.Cmd {
	snap := m.cfg.Snapshot()
	coll := m.coll
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return sysResultMsg{snap: coll.Collect(ctx, snap.Mount, snap.Iface)}
	}
}

// runChecks starts every network check that is not already running. TTFB and
// request time come from a single HTTP request so both describe the same
// transaction.
func (m *Model) runChecks() tea.Cmd {
	if m.paused {
		return nil
	}
	snap := m.cfg.Snapshot()
	p := m.prober
	var cmds []tea.Cmd

	if !m.inflight[config.DNS] {
		m.inflight[config.DNS] = true
		cmds = append(cmds, func() tea.Msg {
			return probeResultMsg{results: []probe.Result{p.DNS(context.Background(), snap)}}
		})
	}
	if !m.inflight[config.Ping] {
		m.inflight[config.Ping] = true
		cmds = append(cmds, func() tea.Msg {
			return probeResultMsg{results: []probe.Result{p.Ping(context.Background(), snap)}}
		})
	}
	if !m.inflight[config.TTFB] && !m.inflight[config.Request] {
		m.inflight[config.TTFB] = true
		m.inflight[config.Request] = true
		cmds = append(cmds, func() tea.Msg {
			ttfb, total := p.HTTP(context.Background(), snap)
			return probeResultMsg{results: []probe.Result{ttfb, total}}
		})
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// resetProbeSeries drops network history, used when the target changes so old
// samples for a different host are not shown alongside new ones.
func (m *Model) resetProbeSeries() {
	for _, k := range config.Probes {
		c := config.MetricChart(k)
		m.series[c].Reset()
		m.detail[c] = ""
		m.errmsg[c] = ""
	}
}

// --- update ---------------------------------------------------------------

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.ready = true
		return m, nil

	case sysTickMsg:
		if msg.epoch != m.sysEpoch {
			return m, nil // superseded by an interval change
		}
		if m.sysInflight {
			// A sample is still being taken; wait for the next tick rather than
			// running a second Collect against the same counter state.
			return m, m.scheduleSystem()
		}
		m.sysInflight = true
		return m, m.collectSystem()

	case sysResultMsg:
		m.sysInflight = false
		m.applySystem(msg.snap)
		return m, m.scheduleSystem()

	case checkTickMsg:
		if msg.epoch != m.checkEpoch {
			return m, nil
		}
		return m, tea.Batch(m.runChecks(), m.scheduleChecks())

	case probeResultMsg:
		for _, r := range msg.results {
			m.applyProbe(r)
		}
		return m, nil

	case svcTickMsg:
		if msg.epoch != m.svcEpoch {
			return m, nil
		}
		if m.svcInflight {
			return m, m.scheduleServices()
		}
		return m, tea.Batch(m.collectServices(), m.scheduleServices())

	case svcResultMsg:
		m.svcInflight = false
		for _, s := range msg.samples {
			m.applyService(s)
		}
		return m, nil

	case discoverMsg:
		return m, m.applyDiscovery(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// applySystem records one system snapshot into the local-metric series.
func (m *Model) applySystem(s metrics.SystemSnapshot) {
	set := func(k config.Metric, v float64, detail string, err error, skip bool) {
		c := config.MetricChart(k)
		if err != nil {
			m.errmsg[c] = err.Error()
			m.series[c].Append(metrics.Sample{At: s.At, OK: false})
			return
		}
		m.errmsg[c] = ""
		m.detail[c] = detail
		if skip {
			return // warm-up sample: the rate is not meaningful yet
		}
		m.series[c].Append(metrics.Sample{At: s.At, Value: v, OK: true})
	}

	set(config.CPU, s.CPUPercent, s.CPUDetail, s.CPUErr, false)
	set(config.Mem, s.MemPercent, s.MemDetail, s.MemErr, false)
	set(config.Disk, s.DiskPercent, s.DiskDetail, s.DiskErr, false)
	set(config.DiskIO, s.DiskIORate, s.DiskIODetail, s.DiskIOErr, s.Warmup)
	set(config.Net, s.NetRate, s.NetDetail, s.NetErr, s.Warmup)
}

// applyService records one service sample.
func (m *Model) applyService(s metrics.ServiceSample) {
	c := config.ServiceChart(s.Name)
	series := m.series[c]
	if series == nil {
		return // the service was removed while its sample was in flight
	}
	if s.Err != nil {
		m.errmsg[c] = s.Err.Error()
		series.Append(metrics.Sample{At: time.Now(), OK: false})
		return
	}
	m.errmsg[c] = ""
	m.detail[c] = fmt.Sprintf("%d pid · %.2f cores · %s",
		s.PIDs, s.Cores, metrics.FormatBytes(float64(s.MemBytes)))
	if s.Warmup {
		return // no previous counter to diff against yet
	}
	series.Append(metrics.Sample{At: time.Now(), Value: s.CPUPercent, OK: true})
}

// applyDiscovery opens the candidate list, or reports why it could not.
func (m *Model) applyDiscovery(msg discoverMsg) tea.Cmd {
	m.scanning = false
	if msg.err != nil {
		m.setStatus("discovery failed: "+msg.err.Error(), true)
		return nil
	}
	if len(msg.candidates) == 0 {
		m.setStatus("no matching processes found", true)
		return nil
	}
	m.candidates = msg.candidates
	m.discoverSel.reset()
	m.discovering = true
	m.overlay = nil
	return nil
}

// addChosenServices registers everything ticked in the discovery list.
func (m *Model) addChosenServices() string {
	names := make([]string, 0, m.discoverSel.count())
	for _, c := range m.candidates {
		if !m.discoverSel.isMarked(c.Name) {
			continue
		}
		name := config.NormaliseServiceName(c.Name)
		m.cfg.Update(func(s *config.Settings) {
			s.AddService(config.Service{Name: name, Match: c.Name})
		})
		m.svcColl.Forget(name)
		names = append(names, name)
	}
	if len(names) == 0 {
		return "nothing selected"
	}
	m.syncSeries(m.cfg.Snapshot())
	return fmt.Sprintf("monitoring %s — shift+tab for the services screen", strings.Join(names, ", "))
}

// applyProbe records one finished network check.
func (m *Model) applyProbe(r probe.Result) {
	m.inflight[r.Metric] = false
	c := config.MetricChart(r.Metric)
	m.series[c].Append(metrics.Sample{At: r.At, Value: r.Value, OK: r.OK})
	if r.OK {
		m.detail[c] = r.Detail
		m.errmsg[c] = ""
		return
	}
	m.errmsg[c] = r.Detail
}

// --- keyboard -------------------------------------------------------------

func (m *Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A modal list owns the keyboard while it is open: keys must not leak into
	// the prompt behind it.
	if m.discovering {
		return m.handleDiscoverKey(k)
	}
	if m.managing {
		return m.handleManageKey(k)
	}
	if m.picker {
		return m.handlePickerKey(k)
	}

	// A keypress dismisses an overlay, except the ones that scroll it.
	if m.overlay != nil {
		switch k.Type {
		case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
			m.scrollOverlay(k.Type)
			return m, nil
		}
		m.dismissOverlay()
		if !passesThroughOverlay(k) {
			return m, nil
		}
	}

	switch k.Type {
	case tea.KeyCtrlC, tea.KeyCtrlD:
		return m, tea.Quit

	case tea.KeyShiftTab:
		m.screen = m.screen.Next()
		return m, nil

	case tea.KeyCtrlR:
		m.setStatus("running checks now", false)
		return m, tea.Batch(m.runChecks(), m.collectServices())

	case tea.KeyCtrlL:
		for _, s := range m.series {
			s.Reset()
		}
		m.setStatus("history cleared", false)
		return m, nil

	case tea.KeyEsc:
		if m.input.Value() != "" {
			m.input.SetValue("")
			m.refreshSuggestions()
			return m, nil
		}
		return m, nil

	case tea.KeyUp:
		if len(m.suggestions) > 0 {
			m.suggIdx = (m.suggIdx - 1 + len(m.suggestions)) % len(m.suggestions)
			return m, nil
		}
		return m, m.browseHistory(-1)

	case tea.KeyDown:
		if len(m.suggestions) > 0 {
			m.suggIdx = (m.suggIdx + 1) % len(m.suggestions)
			return m, nil
		}
		return m, m.browseHistory(1)

	case tea.KeyTab:
		return m, m.acceptSuggestion(false)

	case tea.KeyEnter:
		// Enter runs the command when the first token is already a complete
		// command name; otherwise it accepts the highlighted completion first.
		if m.firstTokenIsCommand() || len(m.suggestions) == 0 {
			return m, m.submit()
		}
		return m, m.acceptSuggestion(true)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	m.refreshSuggestions()
	m.histIdx = len(m.history)
	return m, cmd
}

// handlePickerKey drives the /show list, which spans both screens: you should
// be able to unhide a service without first switching to its screen.
func (m *Model) handlePickerKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	all := m.cfg.Snapshot().AllCharts()
	if len(all) == 0 {
		m.picker = false
		return m, nil
	}
	if m.pickerIdx >= len(all) {
		m.pickerIdx = len(all) - 1
	}

	switch k.Type {
	case tea.KeyCtrlC, tea.KeyCtrlD:
		return m, tea.Quit
	case tea.KeyEsc, tea.KeyEnter:
		m.picker = false
		return m, nil
	case tea.KeyUp:
		m.pickerIdx = (m.pickerIdx - 1 + len(all)) % len(all)
		return m, nil
	case tea.KeyDown:
		m.pickerIdx = (m.pickerIdx + 1) % len(all)
		return m, nil
	case tea.KeySpace:
		m.toggleShown(all[m.pickerIdx])
		return m, nil
	}

	switch strings.ToLower(k.String()) {
	case "k":
		m.pickerIdx = (m.pickerIdx - 1 + len(all)) % len(all)
	case "j":
		m.pickerIdx = (m.pickerIdx + 1) % len(all)
	case "x":
		m.toggleShown(all[m.pickerIdx])
	case "a":
		m.setShown(all, true)
	case "n":
		m.setShown(all, false)
	case "q":
		m.picker = false
	}
	return m, nil
}

// handleDiscoverKey drives the /discover candidate list.
func (m *Model) handleDiscoverKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.candidates)
	if n == 0 {
		m.closeDiscovery()
		return m, nil
	}
	m.discoverSel.clamp(n)

	if d := cursorDelta(k); d != 0 {
		m.discoverSel.move(d, n)
		return m, nil
	}
	switch classify(k) {
	case listQuit:
		return m, tea.Quit
	case listCancel:
		m.closeDiscovery()
		m.setStatus("discovery cancelled", false)
		return m, nil
	case listConfirm:
		msg := m.addChosenServices()
		m.closeDiscovery()
		m.setStatus(msg, false)
		// Sample immediately so the new charts are not blank until the next tick.
		return m, m.collectServices()
	case listToggle:
		m.discoverSel.toggle(m.candidates[m.discoverSel.idx].Name)
		return m, nil
	}

	if strings.EqualFold(k.String(), "l") {
		// Tick everything holding a listening socket: the common case is "chart
		// the servers on this box".
		for _, c := range m.candidates {
			if c.Listening {
				m.discoverSel.mark(c.Name)
			}
		}
	}
	return m, nil
}

func (m *Model) closeDiscovery() {
	m.discovering = false
	m.candidates = nil
	m.discoverSel.reset()
}

// handleManageKey drives the /service list, where a ticked row is one to stop
// charting. Removal only happens on Enter, so a mis-hit space is harmless.
func (m *Model) handleManageKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	svcs := m.cfg.Snapshot().Services
	n := len(svcs)
	m.manageSel.clamp(n)

	if d := cursorDelta(k); d != 0 {
		m.manageSel.move(d, n)
		return m, nil
	}
	switch classify(k) {
	case listQuit:
		return m, tea.Quit
	case listCancel:
		m.closeManage()
		return m, nil
	case listConfirm:
		msg := m.removeMarkedServices()
		m.closeManage()
		if msg != "" {
			m.setStatus(msg, false)
		}
		return m, nil
	case listToggle:
		if n > 0 {
			m.manageSel.toggle(svcs[m.manageSel.idx].Name)
		}
		return m, nil
	}

	if strings.EqualFold(k.String(), "a") && n > 0 {
		// Tick everything, for tearing down a whole set at once.
		for _, svc := range svcs {
			m.manageSel.mark(svc.Name)
		}
	}
	return m, nil
}

func (m *Model) closeManage() {
	m.managing = false
	m.manageSel.reset()
}

// removeMarkedServices stops charting everything ticked in the /service list,
// dropping its series, its threshold and its visibility entry with it.
func (m *Model) removeMarkedServices() string {
	if m.manageSel.count() == 0 {
		return ""
	}
	var removed []string
	for _, svc := range m.cfg.Snapshot().Services {
		if !m.manageSel.isMarked(svc.Name) {
			continue
		}
		name := svc.Name
		ok := false
		m.cfg.Update(func(s *config.Settings) { ok = s.RemoveService(name) })
		if !ok {
			continue
		}
		m.svcColl.Forget(name)
		removed = append(removed, name)
	}
	if len(removed) == 0 {
		return ""
	}
	m.syncSeries(m.cfg.Snapshot())
	return "stopped monitoring " + strings.Join(removed, ", ")
}

// toggleShown flips one chart's visibility.
func (m *Model) toggleShown(c config.ChartID) {
	m.cfg.Update(func(s *config.Settings) { s.Shown[c] = !s.IsShown(c) })
}

// setShown forces a group of charts visible or hidden.
func (m *Model) setShown(cs []config.ChartID, shown bool) {
	m.cfg.Update(func(s *config.Settings) {
		for _, c := range cs {
			s.Shown[c] = shown
		}
	})
}

// passesThroughOverlay reports whether the key that dismissed a panel should
// also reach the prompt. Most keys are swallowed, so closing a panel never
// leaves a stray character to be submitted with the next command; "/" is the
// exception, taking you straight from reading to typing, and the control keys
// keep working throughout.
func passesThroughOverlay(k tea.KeyMsg) bool {
	switch k.Type {
	case tea.KeyCtrlC, tea.KeyCtrlD, tea.KeyCtrlR, tea.KeyCtrlL, tea.KeyShiftTab:
		return true
	case tea.KeyRunes:
		return string(k.Runes) == "/"
	}
	return false
}

func (m *Model) dismissOverlay() {
	m.overlay = nil
	m.overlayTitle = ""
	m.overlayOffset = 0
}

// scrollOverlay moves the panel viewport. The upper bound is clamped at render
// time, which is the only place the visible height is known.
func (m *Model) scrollOverlay(k tea.KeyType) {
	const page = 10
	switch k {
	case tea.KeyUp:
		m.overlayOffset--
	case tea.KeyDown:
		m.overlayOffset++
	case tea.KeyPgUp:
		m.overlayOffset -= page
	case tea.KeyPgDown:
		m.overlayOffset += page
	case tea.KeyHome:
		m.overlayOffset = 0
	case tea.KeyEnd:
		m.overlayOffset = len(m.overlay) // clamped on render
	}
	if m.overlayOffset < 0 {
		m.overlayOffset = 0
	}
}

func (m *Model) browseHistory(delta int) tea.Cmd {
	if len(m.history) == 0 {
		return nil
	}
	idx := m.histIdx + delta
	if idx < 0 {
		idx = 0
	}
	if idx > len(m.history) {
		idx = len(m.history)
	}
	m.histIdx = idx
	if idx == len(m.history) {
		m.input.SetValue("")
	} else {
		m.input.SetValue(m.history[idx])
	}
	m.input.SetCursor(len(m.input.Value()))
	// Recalled commands do not reopen the completion popup: it would capture
	// the next arrow key and trap the user inside a single history entry.
	m.suggestions = nil
	m.suggIdx = 0
	return nil
}

// acceptSuggestion applies the highlighted completion. When runIfComplete is
// set and the completed command takes no arguments, it is executed right away.
func (m *Model) acceptSuggestion(runIfComplete bool) tea.Cmd {
	if len(m.suggestions) == 0 {
		return nil
	}
	s := m.suggestions[m.suggIdx]
	m.input.SetValue(s.replace)
	m.input.SetCursor(len(s.replace))
	m.refreshSuggestions()
	if runIfComplete && s.runNow {
		return m.submit()
	}
	return nil
}

func (m *Model) firstTokenIsCommand() bool {
	fields := splitArgs(m.input.Value())
	if len(fields) == 0 {
		return false
	}
	_, ok := commandByName(fields[0])
	return ok
}

// submit executes whatever is on the prompt.
func (m *Model) submit() tea.Cmd {
	raw := strings.TrimSpace(m.input.Value())
	m.input.SetValue("")
	m.suggestions = nil
	m.suggIdx = 0
	if raw == "" {
		return nil
	}
	m.history = append(m.history, raw)
	m.histIdx = len(m.history)

	fields := splitArgs(raw)
	if len(fields) == 0 {
		return nil
	}
	if !strings.HasPrefix(fields[0], "/") {
		m.setStatus("commands start with / — press / then tab to see them", true)
		return nil
	}
	cmd, found := commandByName(fields[0])
	if !found {
		if near := matchCommands(fields[0]); len(near) > 0 {
			m.setStatus("unknown command "+fields[0]+" — did you mean "+near[0].name+"?", true)
		} else {
			m.setStatus("unknown command "+fields[0]+" — /help lists them all", true)
		}
		return nil
	}
	msg, teaCmd, err := cmd.run(m, fields[1:])
	if err != nil {
		m.setStatus(err.Error(), true)
		return teaCmd
	}
	if msg != "" {
		m.setStatus(msg, false)
	} else {
		m.status = ""
	}
	return teaCmd
}

func (m *Model) setStatus(s string, isErr bool) {
	m.status = s
	m.statusErr = isErr
}

// refreshSuggestions recomputes the completion popup for the current prompt.
func (m *Model) refreshSuggestions() {
	prev := ""
	if m.suggIdx < len(m.suggestions) {
		prev = m.suggestions[m.suggIdx].display
	}
	m.suggestions = m.buildSuggestions()
	m.suggIdx = 0
	// Keep the highlight on the same entry when the list only shrank.
	for i, s := range m.suggestions {
		if s.display == prev {
			m.suggIdx = i
			break
		}
	}
}

func (m *Model) buildSuggestions() []suggestion {
	text := m.input.Value()
	if !strings.HasPrefix(strings.TrimLeft(text, " "), "/") {
		return nil
	}
	fields := splitArgs(text)
	endsWithSpace := strings.HasSuffix(text, " ")

	// Still typing the command name.
	if len(fields) <= 1 && !endsWithSpace {
		prefix := ""
		if len(fields) == 1 {
			prefix = fields[0]
		}
		matches := matchCommands(prefix)
		out := make([]suggestion, 0, len(matches))
		for _, c := range matches {
			replace := c.name
			if c.args != "" {
				replace += " "
			}
			out = append(out, suggestion{
				display: c.signature(),
				desc:    c.desc,
				replace: replace,
				runNow:  c.args == "",
			})
		}
		return out
	}

	cmd, ok := commandByName(fields[0])
	if !ok || cmd.suggest == nil {
		return nil
	}

	var argIdx int
	var prefix string
	if endsWithSpace {
		argIdx = len(fields) - 1
	} else {
		argIdx = len(fields) - 2
		prefix = fields[len(fields)-1]
	}

	// The head is taken from the raw text rather than re-joined from the tokens,
	// so accepting a completion cannot silently strip a quoted argument.
	head := strings.TrimRight(text, " ")
	if !endsWithSpace {
		if i := strings.LastIndexAny(text, " \t"); i >= 0 {
			head = strings.TrimRight(text[:i], " \t")
		} else {
			head = ""
		}
	}
	out := []suggestion{}
	for _, cand := range cmd.suggest(m, argIdx, prefix) {
		out = append(out, suggestion{
			display: cand,
			replace: head + " " + cand + " ",
		})
	}
	return out
}
