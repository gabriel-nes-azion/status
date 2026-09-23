package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"status/internal/config"
	"status/internal/metrics"
)

const (
	// infoWidth is the fixed width of the information block. Every metric's
	// numbers start at the same column and the chart starts right after it, so
	// the whole dashboard reads as one table.
	infoWidth = 30
	// infoHeight is how many lines the information block ever needs. A shorter
	// terminal gets fewer lines per row, never fewer charts: the point of the
	// dashboard is that all of them are on screen at once.
	infoHeight = 5
	// maxRowHeight lets a row grow past the information block when there is
	// height going spare — a screen with four services should spend it on taller
	// charts rather than leave a third of the terminal blank.
	maxRowHeight = 12
	// minChartWidth is the narrowest timeline still worth plotting; below this
	// the information block gives up columns instead.
	minChartWidth = 12
	// gutterWidth is the divider between a service row's two chart columns: a
	// space, the rule, a space, matching the information block's own rule.
	gutterWidth   = 3
	minInfoWidth  = 14
	maxSuggestion = 7
	// pickerRowWidth caps how wide a /show row gets before its value would drift
	// away from its name. Discovery rows carry more per candidate, so they get
	// more room.
	pickerRowWidth   = 46
	discoverRowWidth = 66
	serviceRowWidth  = 72
	// Below this width the dashboard cannot show a usable chart at all.
	minTermW = 34
	// chrome is the number of lines the top bar and the prompt always take, and
	// therefore the height the grid never gets.
	chrome = 4
)

// layout is the resolved geometry for one frame. There is a single column of
// rows, so one set of widths describes every panel.
type layout struct {
	innerW int
	rowH   int
	infoW  int
	chartW int
	chartH int
	gridH  int
	// memW is the width of the second chart column on a service row, and zero
	// when rows carry a single chart — on the main screen, or on a terminal too
	// narrow to split one.
	memW int

	// sections holds only the visible metrics, so the grid never has to consult
	// the hidden set again while rendering.
	sections []config.Section
	rows     int // total visible metrics
	headers  int // one line per visible section
}

func (m *Model) View() string {
	if !m.ready {
		return notice(m.w, "starting…")
	}
	snap := m.cfg.Snapshot()
	sections := snap.SectionsFor(m.screen)
	visible := snap.VisibleCount(m.screen)

	if visible == 0 {
		return notice(m.w, m.emptyScreenLines(snap)...)
	}
	// The height needed depends on what is on screen, so hiding charts is a real
	// way to fit a short terminal rather than a dead end.
	needed := len(sections) + visible + chrome
	if m.w < minTermW || m.h < needed {
		return notice(m.w,
			fmt.Sprintf("%dx%d is too small.", m.w, m.h),
			fmt.Sprintf("%d charts need %dx%d.", visible, minTermW, needed),
			"resize, or /show to hide some.")
	}

	innerW := m.w - 2
	suggLines := len(m.suggestions)
	more := 0
	if suggLines > maxSuggestion {
		more = suggLines - maxSuggestion
		suggLines = maxSuggestion + 1 // room for the "N more" line
	}
	statusLines := 0
	if m.status != "" {
		statusLines = 1
	}

	// The prompt is pinned to the bottom and the grid takes whatever is left.
	// When that is not enough for one line per panel, the blank separator lines
	// are the first thing sacrificed.
	bottomH := 3 + suggLines + statusLines
	spacers := 2
	gridH := m.h - 1 - spacers - bottomH
	if gridH < len(sections)+visible {
		// Not enough room for one line per chart plus its headings: the blank
		// separator lines are the first thing sacrificed.
		spacers = 0
		gridH = m.h - 1 - bottomH
	}
	if gridH < 1 {
		gridH = 1
	}

	l := m.resolveLayout(innerW, gridH, sections, visible)
	m.chartW = l.chartW

	var body string
	switch {
	case m.discovering:
		body = m.renderDiscovery(innerW, gridH)
	case m.managing:
		body = m.renderServiceList(innerW, gridH)
	case m.picker:
		body = m.renderPicker(innerW, gridH)
	case m.overlay != nil:
		body = m.renderOverlay(innerW, gridH)
	default:
		body = m.renderGrid(l)
	}

	parts := []string{m.renderTopBar(innerW, l)}
	if spacers > 0 {
		parts = append(parts, "")
	}
	parts = append(parts, body)
	if spacers > 0 {
		parts = append(parts, "")
	}
	if statusLines == 1 {
		parts = append(parts, m.renderStatus(innerW))
	}
	parts = append(parts, m.renderPrompt(innerW))
	if suggLines > 0 {
		parts = append(parts, m.renderSuggestions(innerW, more))
	}

	out := strings.Join(parts, "\n")
	return lipgloss.NewStyle().PaddingLeft(1).Render(out)
}

// resolveLayout sizes the single column of rows. The information block keeps a
// fixed width so the numbers line up down the screen; the chart takes whatever
// is left, which is what makes the visible time window as long as the terminal
// allows.
func (m *Model) resolveLayout(innerW, gridH int, sections []config.Section, rows int) layout {
	headers := len(sections)

	rowH := 1
	if rows > 0 {
		rowH = (gridH - headers) / rows
	}
	if rowH > maxRowHeight {
		rowH = maxRowHeight
	}
	if rowH < 1 {
		rowH = 1
	}

	infoW := infoWidth
	if innerW-infoW < minChartWidth {
		infoW = innerW - minChartWidth
	}
	if infoW < minInfoWidth {
		infoW = minInfoWidth
	}
	if infoW > innerW-1 {
		infoW = innerW - 1
	}

	// The row's last line stays blank in the chart column so stacked charts do
	// not merge; a one-line row has no line to spare.
	chartH := rowH - 1
	if rowH == 1 {
		chartH = 1
	}

	// A service row answers two questions, so its chart area is split in two:
	// CPU share on the left, resident memory on the right. Below the width where
	// both halves would still be readable the memory column is dropped rather
	// than both timelines squeezed into noise.
	chartW, memW := innerW-infoW, 0
	if m.screen == config.ScreenServices {
		if avail := chartW - gutterWidth; avail >= 2*minChartWidth {
			memW = avail / 2
			chartW = avail - memW // the odd column goes to CPU, the primary one
		}
	}

	return layout{
		innerW:   innerW,
		rowH:     rowH,
		infoW:    infoW,
		chartW:   chartW,
		memW:     memW,
		chartH:   chartH,
		gridH:    gridH,
		sections: sections,
		rows:     rows,
		headers:  headers,
	}
}

func (m *Model) renderGrid(l layout) string {
	snap := m.cfg.Snapshot()
	lines := make([]string, 0, l.headers+l.rows)
	for _, sec := range l.sections {
		lines = append(lines, sectionHeading(sec.Name, l.innerW))
		for _, c := range sec.Charts {
			lines = append(lines, m.renderPanel(c, l, snap))
		}
	}
	grid := strings.Join(lines, "\n")

	// Pad to the reserved height so the prompt stays anchored at the bottom.
	if pad := l.gridH - (l.headers + l.rows*l.rowH); pad > 0 {
		grid += strings.Repeat("\n", pad)
	}
	return grid
}

// screenTabs renders the screen switcher, marking the count of charts on each so
// the services screen advertises itself even while you are on the main one.
func (m *Model) screenTabs(snap config.Settings) string {
	parts := make([]string, 0, len(config.Screens))
	for _, sc := range config.Screens {
		label := fmt.Sprintf("%s(%d)", strings.ToUpper(sc.String()), snap.VisibleCount(sc))
		if sc == m.screen {
			parts = append(parts, styAccentB.Render(label))
			continue
		}
		parts = append(parts, styFaint.Render(label))
	}
	return strings.Join(parts, styFaint.Render("/"))
}

// sectionHeading labels a group of metrics and rules off to the right edge, so
// the split between local resources and remote checks is unmissable.
func sectionHeading(name string, w int) string {
	label := styAccentB.Render(strings.ToUpper(name))
	rest := w - width(label) - 1
	if rest < 1 {
		return label
	}
	return label + " " + styFaint.Render(strings.Repeat("─", rest))
}

func (m *Model) renderPanel(c config.ChartID, l layout, snap config.Settings) string {
	th, hasTh := snap.Threshold(c)
	p := panel{
		chart:     c,
		title:     snap.ChartTitle(c),
		series:    m.series[c],
		detail:    m.detail[c],
		lastErr:   m.errmsg[c],
		threshold: th,
		hasThresh: hasTh,
		rowH:      l.rowH,
		infoW:     l.infoW,
		chartW:    l.chartW,
		chartH:    l.chartH,
	}
	if c.IsService() {
		p.memSeries, p.memW = m.memSeries[c], l.memW
	}
	return p.render()
}

func (m *Model) renderTopBar(w int, l layout) string {
	snap := m.cfg.Snapshot()
	chartW := l.chartW

	sep := styFaint.Render("  ·  ")
	// Segments are listed most to least important; the least important are
	// dropped one at a time until the bar fits.
	segments := []string{
		styAccentB.Render("STATUS"),
		m.screenTabs(snap),
		styText.Render(snap.URL()),
		styDim.Render("checks " + shortDur(snap.Interval)),
		styDim.Render("sys " + shortDur(snap.SysInterval)),
		styDim.Render(fmt.Sprintf("window %s/%s",
			shortDur(time.Duration(chartW)*snap.SysInterval),
			shortDur(time.Duration(chartW)*snap.Interval))),
	}

	alerts, hidden := m.alertCount(snap)
	stateText, stateSty := "● OK", styOK
	var extra string
	switch {
	case m.paused:
		stateText, stateSty = "⏸ PAUSED", styPaused
	case alerts > 0:
		word := "ALERT"
		if alerts > 1 {
			word = "ALERTS"
		}
		stateText, stateSty = fmt.Sprintf("▲ %d %s", alerts, word), styAlertB
		if hidden > 0 {
			// Hiding a chart, or leaving it on the other screen, must not
			// silence it.
			extra = styAlert.Render(fmt.Sprintf(" (+%d unseen)", hidden))
		}
	case hidden > 0:
		stateText, stateSty = fmt.Sprintf("▲ %d UNSEEN", hidden), styAlertB
	}
	state := stateSty.Render(stateText)
	clock := styFaint.Render("  " + time.Now().Format("15:04:05"))

	// The right-hand side gives ground too on a narrow terminal, least
	// important first: the clock, then the unseen count. The state itself is
	// the one thing on the bar that must never be dropped.
	right := state + extra + clock
	for _, cand := range []string{state + extra, state} {
		if width(right) <= w {
			break
		}
		right = cand
	}
	rw := width(right)

	left := strings.Join(segments, sep)
	for len(segments) > 1 && w-width(left)-rw < 2 {
		segments = segments[:len(segments)-1]
		left = strings.Join(segments, sep)
	}
	gap := w - width(left) - rw
	if gap < 1 {
		// Only the host is left and it still does not fit: truncate it.
		left = styAccentB.Render("STATUS") + sep + styText.Render(truncate(snap.Host, max(1, w-rw-12)))
		gap = w - width(left) - rw
	}
	if gap < 1 {
		// Not even the state and a one-cell gap fit. Everything but the state
		// goes, and the state itself is cut to the terminal rather than wrapped:
		// a bar that overruns takes the whole frame with it.
		return stateSty.Render(truncate(stateText, w))
	}
	return left + strings.Repeat(" ", gap) + right
}

// alertCount returns how many charts are alerting, split into those visible on
// the current screen and those the user cannot currently see — hidden, or parked
// on the other screen. Neither hiding a chart nor switching screens may silence
// it, so the second number is surfaced too.
func (m *Model) alertCount(snap config.Settings) (visible, unseen int) {
	for _, c := range snap.AllCharts() {
		if !m.isAlerting(c, snap) {
			continue
		}
		if snap.IsShown(c) && snap.ScreenOf(c) == m.screen {
			visible++
		} else {
			unseen++
		}
	}
	return visible, unseen
}

func (m *Model) isAlerting(c config.ChartID, snap config.Settings) bool {
	series := m.series[c]
	if series == nil {
		return false
	}
	th, hasTh := snap.Threshold(c)
	return panel{series: series, threshold: th, hasThresh: hasTh}.alerting()
}

// emptyScreenLines explains an empty screen and how to fill it, which differs
// between "you hid everything" and "you have not discovered any services".
func (m *Model) emptyScreenLines(snap config.Settings) []string {
	if m.screen == config.ScreenServices && len(snap.Services) == 0 {
		return []string{
			"no services charted yet.",
			"/discover scans this machine and lets you pick.",
			"shift+tab returns to the main screen.",
		}
	}
	return []string{
		"every chart on this screen is hidden.",
		"type /show to bring some back.",
	}
}

func (m *Model) renderStatus(w int) string {
	sty, mark := styDim, "✓ "
	if m.statusErr {
		sty, mark = styAlert, "✕ "
	}
	return sty.Render(truncate(mark+m.status, w))
}

func (m *Model) renderPrompt(w int) string {
	inner := w - 4 // rounded border (2) + prompt marker (2)
	if inner < 8 {
		inner = 8
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colFaint).
		Width(w-2).
		Padding(0, 1)

	// The modal lists own the keyboard, so the prompt has to look inactive: a
	// live cursor would invite typing that gets read as list keys.
	if hint := m.modalHint(); hint != "" {
		return box.Render(styFaint.Render("❯ " + truncate(hint, inner)))
	}

	m.input.Width = inner - 1
	return box.Render(styAccent.Render("❯ ") + m.input.View())
}

// modalHint is the placeholder shown while a modal list has the keyboard.
func (m *Model) modalHint() string {
	switch {
	case m.discovering:
		return "picking services — enter to chart them, esc to cancel"
	case m.managing:
		return "managing services — enter to apply, esc to cancel"
	case m.picker:
		return "selecting charts — esc to return to the prompt"
	}
	return ""
}

func (m *Model) renderSuggestions(w int, more int) string {
	shown := m.suggestions
	if more > 0 {
		shown = shown[:maxSuggestion]
	}
	sigW := 0
	for _, s := range shown {
		if n := width(s.display); n > sigW {
			sigW = n
		}
	}
	if sigW > w/2 {
		sigW = w / 2
	}

	lines := make([]string, 0, len(shown)+1)
	for i, s := range shown {
		marker, nameSty := "  ", styDim
		if i == m.suggIdx {
			marker, nameSty = styAccent.Render("❯ "), styAccentB
		}
		line := marker + nameSty.Render(pad(s.display, sigW))
		if s.desc != "" {
			line += "  " + styFaint.Render(truncate(s.desc, w-sigW-6))
		}
		lines = append(lines, line)
	}
	if more > 0 {
		lines = append(lines, "  "+styFaint.Render(fmt.Sprintf("… %d more", more)))
	}
	return strings.Join(lines, "\n")
}

// shortDur renders a duration without trailing zero units ("5m" not "5m0s").
func shortDur(d time.Duration) string {
	switch {
	case d >= time.Hour:
		if d%time.Hour == 0 {
			return fmt.Sprintf("%dh", int(d.Hours()))
		}
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		if d%time.Minute == 0 {
			return fmt.Sprintf("%dm", int(d.Minutes()))
		}
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	case d >= time.Second:
		if d%time.Second == 0 {
			return fmt.Sprintf("%ds", int(d.Seconds()))
		}
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
}

func (m *Model) renderOverlay(w, h int) string {
	lines := m.overlay
	avail := h - 2 // the box border
	if avail < 1 {
		avail = 1
	}

	if len(lines) <= avail {
		m.overlayOffset = 0
		return m.modalBox(lines, w, h, colAccent)
	}

	// One line of the viewport goes to the scroll indicator, so the reader can
	// tell there is more and how to reach it.
	view := avail - 1
	maxOffset := len(lines) - view
	if m.overlayOffset > maxOffset {
		m.overlayOffset = maxOffset
	}
	from := m.overlayOffset
	shown := append([]string(nil), lines[from:from+view]...)

	hint := fmt.Sprintf("  %d–%d of %d · ↑↓ pgup/pgdn to scroll · any other key closes",
		from+1, from+view, len(lines))
	return m.modalBox(append(shown, styFaint.Render(hint)), w, h, colAccent)
}

// renderPicker draws the interactive /show list. Unlike the static overlays it
// is rebuilt every frame, so a toggle is reflected immediately, and it reports
// each chart's current value and alert state — you should be able to see what
// you are about to hide.
//
// It spans both screens: hiding a service should not require switching to the
// services screen first.
//
// Every chart must stay on the list even on a short terminal: a picker that
// hides the thing you came to unhide is useless. So the headings, the blank
// separator and the key hint are given up first, in that order.
func (m *Model) renderPicker(w, h int) string {
	snap := m.cfg.Snapshot()
	rowW := clampRowWidth(w, pickerRowWidth)

	type group struct {
		name   string
		screen config.Screen
		charts []config.ChartID
	}
	var groups []group
	rows := 0
	for _, screen := range config.Screens {
		for _, sec := range snap.AllSections(screen) {
			if len(sec.Charts) == 0 {
				continue
			}
			groups = append(groups, group{name: sec.Name, screen: screen, charts: sec.Charts})
			rows += len(sec.Charts)
		}
	}

	avail := h - 2 // the box border
	headings := len(groups)
	blanks := headings - 1
	withHeadings := avail >= rows+headings
	withBlank := avail >= rows+headings+blanks
	withHint := avail >= rows+headings+blanks+2

	lines := make([]string, 0, rows+headings+blanks+2)
	idx := 0
	for gi, g := range groups {
		if gi > 0 && withBlank {
			lines = append(lines, "")
		}
		if withHeadings {
			label := section(g.name)
			if g.screen != m.screen {
				// Say where a chart lives, so toggling one that does not appear
				// is not mistaken for a bug.
				label += styFaint.Render("  (" + g.screen.String() + " screen)")
			}
			lines = append(lines, label)
		}
		for _, c := range g.charts {
			lines = append(lines, m.pickerRow(c, snap, idx == m.pickerIdx, rowW))
			idx++
		}
	}
	if withHint {
		lines = append(lines, "",
			styFaint.Render("  space toggle · a show all · n hide all · ↑↓ move · esc close"))
	}
	if len(lines) > avail && avail > 0 {
		lines = append(lines[:avail-1:avail-1], styFaint.Render("  … resize to see the rest"))
	}
	return m.modalBox(lines, w, h, colAccent)
}

func (m *Model) pickerRow(c config.ChartID, snap config.Settings, selected bool, w int) string {
	d := descriptorFor(c)
	shown := snap.IsShown(c)

	cursor := "  "
	nameSty := styText
	if selected {
		cursor = styAccent.Render("❯ ")
		nameSty = styAccentB
	}
	box := styOK.Render("[✓]")
	if !shown {
		box = styFaint.Render("[ ]")
		if !selected {
			nameSty = styDim
		}
	}

	th, hasTh := snap.Threshold(c)
	p := panel{series: m.series[c], threshold: th, hasThresh: hasTh}
	value, valueSty := p.valueText(d)
	if p.alerting() {
		value = "▲ " + value
	}

	left := cursor + box + " " + nameSty.Render(snap.ChartTitle(c))
	return "  " + lr(left, valueSty.Render(value), w)
}

// renderDiscovery draws the /discover result list: candidate process groups with
// enough context to decide, ranked so the ones holding listening sockets — the
// best available signal for "this is a service" — come first.
func (m *Model) renderDiscovery(w, h int) string {
	snap := m.cfg.Snapshot()
	avail := h - 2

	header := fmt.Sprintf("%d candidates · %d selected", len(m.candidates), m.discoverSel.count())
	lines := []string{
		section("discovered"),
		styFaint.Render("  " + header),
		"",
	}
	hint := []string{"",
		styFaint.Render("  space select · l select all listening · enter chart them · esc cancel")}

	rowW := clampRowWidth(w, discoverRowWidth)
	body := avail - len(lines) - len(hint)
	if body < 1 {
		body = 1
	}

	// Keep the cursor on screen when the list is longer than the box.
	from := 0
	if m.discoverSel.idx >= body {
		from = m.discoverSel.idx - body + 1
	}
	to := from + body
	if to > len(m.candidates) {
		to = len(m.candidates)
	}

	for i := from; i < to; i++ {
		lines = append(lines, m.candidateRow(i, snap, rowW))
	}
	if to < len(m.candidates) {
		lines[len(lines)-1] = styFaint.Render(fmt.Sprintf("  … %d more below", len(m.candidates)-to+1))
	}
	if avail >= len(lines)+len(hint) {
		lines = append(lines, hint...)
	}
	return m.modalBox(lines, w, h, colAccent)
}

func (m *Model) candidateRow(i int, snap config.Settings, w int) string {
	c := m.candidates[i]

	cursor := "  "
	nameSty := styText
	if i == m.discoverSel.idx {
		cursor = styAccent.Render("❯ ")
		nameSty = styAccentB
	}
	box := styFaint.Render("[ ]")
	if m.discoverSel.isMarked(c.Name) {
		box = styOK.Render("[✓]")
	}

	name := c.Name
	// Flag what is already charted so the list does not invite duplicates.
	if _, exists := snap.Service(config.NormaliseServiceName(c.Name)); exists {
		name += " ✓"
		nameSty = styDim
		if i == m.discoverSel.idx {
			nameSty = styAccent
		}
	}

	mark := "  "
	if c.Listening {
		mark = styOK.Render("◆ ")
	}
	left := cursor + box + " " + mark + nameSty.Render(name)

	// The CPU figure is a lifetime average, which is the wrong thing to chart but
	// the right thing to rank by, so it is shown here and nowhere else.
	right := fmt.Sprintf("%.0f%% · %d pid · %s",
		c.CPUPercent, c.PIDs, metrics.FormatBytes(float64(c.MemBytes)))
	if ports := metrics.FormatPorts(c.Ports); ports != "" {
		right = ports + " · " + right
	}
	return "  " + lr(left, styDim.Render(right), w)
}

// modalBox frames a modal list and pads it to the reserved height so the prompt
// below does not move.
func (m *Model) modalBox(lines []string, w, h int, border lipgloss.Color) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Width(w-2).
		Padding(0, 1)
	rendered := box.Render(strings.Join(lines, "\n"))
	if pad := h - lipgloss.Height(rendered); pad > 0 {
		rendered += strings.Repeat("\n", pad)
	}
	return rendered
}

// clampRowWidth keeps a modal row's value next to its name instead of stranding
// the two at opposite edges of a wide terminal.
func clampRowWidth(w, max int) int {
	rowW := w - 6
	if rowW > max {
		rowW = max
	}
	if rowW < 12 {
		rowW = 12
	}
	return rowW
}

// renderServiceList draws the /service list: what is being charted, and which
// rows are ticked to stop being charted.
//
// The tick means "remove" here and "keep" in the /show picker, so it is drawn as
// a red ✗ rather than a check, and the heading says so. Two similar-looking lists
// with inverted checkboxes would be a trap.
func (m *Model) renderServiceList(w, h int) string {
	snap := m.cfg.Snapshot()
	svcs := snap.Services
	avail := h - 2 // the box border

	if len(svcs) == 0 {
		return m.modalBox([]string{
			section("services"),
			"",
			"  " + styDim.Render("nothing charted yet."),
			"",
			"  " + styAccent.Render(pad("/discover", 22)) + styDim.Render("scan this machine and pick from the list"),
			"  " + styAccent.Render(pad("/service add <name>", 22)) + styDim.Render("chart a process group by hand"),
			"",
			styFaint.Render("  esc closes"),
		}, w, h, colAccent)
	}

	marked := m.manageSel.count()
	head := fmt.Sprintf("%d charted", len(svcs))
	if marked > 0 {
		head += fmt.Sprintf(" · %d to remove", marked)
	}
	lines := []string{section("services"), styFaint.Render("  " + head), ""}

	hint := []string{"", styFaint.Render(
		"  space mark · a mark all · enter stop monitoring the marked · esc cancel")}

	rowW := clampRowWidth(w, serviceRowWidth)
	cols := serviceColumns(m, svcs, snap, rowW)
	body := avail - len(lines) - len(hint)
	if body < 1 {
		body = 1
	}

	// Keep the cursor on screen when the list is longer than the box.
	from := 0
	if m.manageSel.idx >= body {
		from = m.manageSel.idx - body + 1
	}
	to := from + body
	if to > len(svcs) {
		to = len(svcs)
	}
	for i := from; i < to; i++ {
		lines = append(lines, m.serviceRow(svcs[i], snap, i == m.manageSel.idx, rowW, cols))
	}
	if to < len(svcs) {
		lines[len(lines)-1] = styFaint.Render(fmt.Sprintf("  … %d more below", len(svcs)-to+1))
	}
	if avail >= len(lines)+len(hint) {
		lines = append(lines, hint...)
	}
	return m.modalBox(lines, w, h, colAccent)
}

// serviceCols are the column widths of the service list, measured once over
// every row so the columns line up instead of shifting with each row's content.
type serviceCols struct {
	name  int
	match int
	right int
}

func serviceColumns(m *Model, svcs []config.Service, snap config.Settings, w int) serviceCols {
	c := serviceCols{name: 8}
	for _, svc := range svcs {
		if n := width(serviceRowName(svc, snap)); n > c.name {
			c.name = n
		}
		if n := width(serviceRowRight(m, svc, snap)); n > c.right {
			c.right = n
		}
	}
	if max := w / 3; c.name > max {
		c.name = max
	}
	// The row is: cursor(2) + checkbox(3) + space + name + space + match, then at
	// least one space before the right column. Getting this budget wrong by one
	// makes lr drop the right column on the widest row rather than wrap it.
	const fixed = 2 + 3 + 1 + 1 + 1
	c.match = w - fixed - c.name - c.right
	if c.match < 0 {
		c.match = 0
	}
	return c
}

// serviceRowName is the name column's plain text.
func serviceRowName(svc config.Service, snap config.Settings) string {
	if snap.IsShown(config.ServiceChart(svc.Name)) {
		return svc.Name
	}
	return svc.Name + " (hidden)"
}

// serviceRowRight is the threshold and current value, as plain text, for
// measuring the column.
func serviceRowRight(m *Model, svc config.Service, snap config.Settings) string {
	id := config.ServiceChart(svc.Name)
	th, hasTh := snap.Threshold(id)
	p := panel{series: m.series[id], threshold: th, hasThresh: hasTh}
	value, _ := p.valueText(descriptorFor(id))
	if p.alerting() {
		value = "▲ " + value
	}
	if hasTh {
		return "thr " + formatThreshold(id, th) + " · " + value
	}
	return value
}

func (m *Model) serviceRow(svc config.Service, snap config.Settings, selected bool, w int, cols serviceCols) string {
	id := config.ServiceChart(svc.Name)
	d := descriptorFor(id)
	doomed := m.manageSel.isMarked(svc.Name)

	cursor := "  "
	nameSty := styText
	if selected {
		cursor = styAccent.Render("❯ ")
		nameSty = styAccentB
	}

	box := styFaint.Render("[ ]")
	if doomed {
		box = styAlert.Render("[✗]")
		nameSty = styAlert
		if selected {
			nameSty = styAlertB
		}
	} else if !snap.IsShown(id) && !selected {
		// A hidden chart is still charted; dim it the way the picker does.
		nameSty = styDim
	}

	th, hasTh := snap.Threshold(id)
	p := panel{series: m.series[id], threshold: th, hasThresh: hasTh}
	value, valueSty := p.valueText(d)
	if p.alerting() {
		value = "▲ " + value
	}

	right := valueSty.Render(value)
	if hasTh {
		right = styThreshLn.Render("thr "+formatThreshold(id, th)) +
			styFaint.Render(" · ") + right
	}
	// Padded to the measured column so every row is the same width and the
	// labels line up, rather than each row's right edge floating with its
	// content.
	right = padStyled(right, cols.right)

	// The match is the middle column: it is what you check before removing the
	// wrong one of two similarly named services.
	name := truncate(serviceRowName(svc, snap), cols.name)
	line := cursor + box + " " + padStyled(nameSty.Render(name), cols.name)
	if cols.match > 4 {
		line += " " + padStyled(styDim.Render(truncate(svc.Match, cols.match)), cols.match)
	}
	return "  " + lr(line, right, w)
}
