package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"status/internal/config"
	"status/internal/metrics"
)

// The EDGE row plots three timelines in one chart area, stacked top to bottom:
// orchestration latency, the location-PoP that answered and the HTTP status. The
// latter two are categorical, so each is drawn as a run of rule glyphs coloured
// by value, labelled where the value changes. A shorter row first merges status
// and loc-pop into one lane, then marks rule changes on that lane itself.

const (
	laneGlyph  = '─'
	eventGlyph = '▲'
)

func isEdge(c config.ChartID) bool { return c == config.MetricChart(config.Edge) }

// edgeCell is one column of a lane. Styles are compared by pointer when
// grouping runs, so they must come from package vars or a per-render cache.
type edgeCell struct {
	r   rune
	sty *lipgloss.Style
}

func blankCells(w int) []edgeCell {
	cells := make([]edgeCell, w)
	for i := range cells {
		cells[i].r = ' '
	}
	return cells
}

func renderCells(cells []edgeCell) string {
	var b, run strings.Builder
	var runSty *lipgloss.Style
	flush := func() {
		if run.Len() == 0 {
			return
		}
		if runSty == nil {
			b.WriteString(run.String())
		} else {
			b.WriteString(runSty.Render(run.String()))
		}
		run.Reset()
	}
	for _, c := range cells {
		if c.sty != runSty {
			flush()
			runSty = c.sty
		}
		run.WriteRune(c.r)
	}
	flush()
	return b.String()
}

// edgeLanes renders the chart area of the EDGE row: exactly chartH lines of
// chartW cells.
func (p panel) edgeLanes(d descriptor, max float64) []string {
	w, h := p.chartW, p.chartH
	if w < 1 || h < 1 {
		return nil
	}
	samples := p.series.Tail(w)
	offset := w - len(samples)
	pops := map[string]*lipgloss.Style{}

	status := func(s metrics.Sample) (string, *lipgloss.Style) {
		return statusText(s.Code), statusStyle(s.Code)
	}
	locPop := func(s metrics.Sample) (string, *lipgloss.Style) {
		return locPopText(s.Label), popStyle(pops, s.Label)
	}
	both := func(s metrics.Sample) (string, *lipgloss.Style) {
		return statusText(s.Code) + " " + locPopText(s.Label), statusStyle(s.Code)
	}

	switch h {
	case 1:
		cells := categoryCells(samples, offset, w, both)
		markEvents(cells, samples, offset, lipgloss.NewStyle().Foreground(d.fg))
		return []string{renderCells(cells)}
	case 2:
		return []string{
			eventLane(samples, offset, w, d),
			categoryLane(samples, offset, w, both),
		}
	}
	out := make([]string, 0, h)
	if k := h - 3; k > 0 {
		out = append(out, chart{samples: samples, width: w, height: k, max: max, fg: d.fg}.render()...)
	}
	return append(out,
		eventLane(samples, offset, w, d),
		categoryLane(samples, offset, w, locPop),
		categoryLane(samples, offset, w, status),
	)
}

// categoryLane draws one categorical timeline. A value's label is written where
// it starts, and at the left edge for the run already under way, whenever it
// fits before the next change.
func categoryLane(samples []metrics.Sample, offset, w int, value func(metrics.Sample) (string, *lipgloss.Style)) string {
	return renderCells(categoryCells(samples, offset, w, value))
}

func categoryCells(samples []metrics.Sample, offset, w int, value func(metrics.Sample) (string, *lipgloss.Style)) []edgeCell {
	cells := blankCells(w)
	type run struct {
		col   int
		label string
		sty   *lipgloss.Style
	}
	var runs []run
	prev := ""
	for i, s := range samples {
		col := offset + i
		if !s.OK {
			cells[col] = edgeCell{outageGlyph, &styAlert}
			prev = ""
			continue
		}
		label, sty := value(s)
		cells[col] = edgeCell{laneGlyph, sty}
		if label != prev {
			runs = append(runs, run{col, label, sty})
		}
		prev = label
	}
	for i, r := range runs {
		end := w
		if i+1 < len(runs) {
			end = runs[i+1].col
		}
		label := []rune(r.label)
		if len(label) >= end-r.col {
			continue
		}
		for j, ch := range label {
			if cells[r.col+j].r == outageGlyph {
				break
			}
			cells[r.col+j] = edgeCell{ch, r.sty}
		}
	}
	return cells
}

// markEvents puts a rule-change marker on a categorical lane wherever it does
// not cover a label.
func markEvents(cells []edgeCell, samples []metrics.Sample, offset int, sty lipgloss.Style) {
	for i, s := range samples {
		if col := offset + i; s.OK && s.Value != 0 && cells[col].r == laneGlyph {
			cells[col] = edgeCell{eventGlyph, &sty}
		}
	}
}

// eventLane marks every rule change seen while monitoring, with its propagation
// time written to the left of the marker so the newest one, at the right edge,
// is always labelled.
func eventLane(samples []metrics.Sample, offset, w int, d descriptor) string {
	cells := blankCells(w)
	sty := lipgloss.NewStyle().Foreground(d.fg)
	prevEnd := 0
	for i, s := range samples {
		if !s.OK || s.Value == 0 {
			continue
		}
		col := offset + i
		cells[col] = edgeCell{eventGlyph, &sty}
		label := []rune(formatOrch(s.Value))
		if start := col - len(label); start >= prevEnd {
			for j, ch := range label {
				cells[start+j] = edgeCell{ch, &sty}
			}
		}
		prevEnd = col + 2 // keep a gap before the next label
	}
	return renderCells(cells)
}

// edgeInfoText is the EDGE row's information block: the newest status and
// loc-pop, then the propagation time of the last rule change, which reads "-"
// until one happens while the dashboard is watching.
func (p panel) edgeInfoText(d descriptor, stats metrics.Stats, max float64, w, ih int) []string {
	const indent = "  "
	head := p.headText(d, w)
	axis := styFaint.Render("⌃" + d.unit.FormatAxis(max))

	code, status := styDim.Render("—"), styDim.Render("—")
	if last, ok := p.series.Last(); ok {
		switch {
		case !last.OK:
			code = styAlertB.Render("FAIL")
			status = code
		default:
			code = statusStyle(last.Code).Bold(true).Render(statusText(last.Code))
			status = code + styFaint.Render(" · ") + styText.Render(locPopText(last.Label))
		}
	}

	orch := styDim.Render("orch ")
	if p.hasOrch {
		orch += lipgloss.NewStyle().Foreground(d.fg).Bold(true).Render(formatOrch(p.edgeOrch))
	} else {
		orch += styDim.Render("-")
	}
	orchLine := lr(indent+orch, p.failText(stats), w)

	switch {
	case ih >= 4:
		lines := []string{lr(head, axis, w), indent + status, orchLine}
		return append(lines, p.detailLines(w, indent, ih-len(lines))...)
	case ih == 3:
		return []string{lr(head, axis, w), indent + status, orchLine}
	case ih == 2:
		return []string{lr(head, status, w), orchLine}
	default:
		// The one-line lane already names the loc-pop, so the line keeps the
		// status and gives the rest to the propagation time.
		return []string{lr(head, code+styFaint.Render(" · ")+orch, w)}
	}
}

func statusText(code int) string {
	if code == 0 {
		return "—"
	}
	return strconv.Itoa(code)
}

func statusStyle(code int) *lipgloss.Style {
	switch {
	case code >= 500:
		return &styAlert
	case code >= 400:
		return &styThreshLn
	case code >= 300:
		return &styAccent
	case code >= 200:
		return &styOK
	}
	return &styDim
}

func locPopText(label string) string {
	if label == "" {
		return "?"
	}
	return label
}

// popStyle colours a loc-pop by a hash of its name, so the same edge keeps its
// colour across runs and a switch between edges shows as a change of hue.
func popStyle(cache map[string]*lipgloss.Style, label string) *lipgloss.Style {
	if label == "" {
		return &styFaint
	}
	if s, ok := cache[label]; ok {
		return s
	}
	s := lipgloss.NewStyle().Foreground(serviceColor(label))
	cache[label] = &s
	return &s
}

// formatOrch renders a propagation time given in milliseconds.
func formatOrch(ms float64) string {
	sign := ""
	if ms < 0 {
		sign, ms = "-", -ms
	}
	sec := ms / 1000
	if sec < 60 {
		return fmt.Sprintf("%s%.1fs", sign, sec)
	}
	return sign + shortDur(time.Duration(math.Round(sec))*time.Second)
}
