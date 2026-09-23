package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"status/internal/config"
	"status/internal/metrics"
)

// panel is one metric row: a fixed-width information block on the left, so every
// metric's numbers line up in a single readable column, and the timeline chart
// filling the rest of the line.
//
// A service row carries two charts instead of one — CPU on the left, resident
// memory on the right — because "is this service busy" and "is this service
// growing" are different questions and the answers are only useful together.
//
// The bottom line of a row is deliberately left blank in the chart column: with
// consecutive charts touching, nine stacked area charts read as one solid block.
type panel struct {
	chart     config.ChartID
	title     string
	series    *metrics.Series
	detail    string
	lastErr   string
	threshold float64
	hasThresh bool

	// memSeries is the second column's data, set only for service rows. With
	// memW it decides whether the chart area is split at all: a terminal too
	// narrow for two readable timelines keeps the single CPU one.
	memSeries *metrics.Series

	rowH   int // total lines in the row, including the chart's blank last line
	infoW  int // width of the information block; the chart starts at this offset
	chartW int
	chartH int
	memW   int
}

// split reports whether this row draws the memory column beside the CPU one.
func (p panel) split() bool { return p.memW > 0 && p.memSeries != nil }

// alerting reports whether the most recent sample breaches the threshold or the
// check failed, which is what turns the row red.
func (p panel) alerting() bool {
	last, ok := p.series.Last()
	if !ok {
		return false
	}
	if !last.OK {
		return true
	}
	return p.hasThresh && last.Value >= p.threshold
}

func (p panel) render() string {
	d := descriptorFor(p.chart)
	stats := p.series.Stats(p.chartW)
	max := axisMax(d, stats.Max)

	info := p.info(d, stats, max)
	rows := p.chartColumns(d, max)

	lines := make([]string, p.rowH)
	for i := range lines {
		line := info[i]
		if i < len(rows) {
			line += rows[i]
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// info renders the left block: exactly rowH lines, each exactly infoW cells.
// The vertical rule marks where the chart begins and is drawn only next to the
// chart's own rows, which is what visually groups each metric.
func (p panel) info(d descriptor, stats metrics.Stats, max float64) []string {
	textW := p.infoW - 3 // text, a space, the rule, a space
	withRule := textW >= 8
	if !withRule {
		textW = p.infoW
	}

	texts := p.infoText(d, stats, max, textW)
	out := make([]string, p.rowH)
	for i := range out {
		txt := ""
		if i < len(texts) {
			txt = texts[i]
		}
		if !withRule {
			out[i] = padStyled(txt, p.infoW)
			continue
		}
		rule := " "
		if i < p.chartH {
			rule = styFaint.Render("│")
		}
		out[i] = padStyled(txt, textW) + " " + rule + " "
	}
	return out
}

// infoText builds the text lines of the block, densest layout first. Shorter
// rows drop the least useful line rather than shrinking every line; a row taller
// than the block needs simply leaves the surplus to the chart.
func (p panel) infoText(d descriptor, stats metrics.Stats, max float64, w int) []string {
	const indent = "  "

	ih := p.rowH
	if ih > infoHeight {
		ih = infoHeight
	}

	value, valueSty := p.valueText(d)
	head := p.headText(d, w)
	axis := styFaint.Render("⌃" + d.unit.FormatAxis(max))
	valueLine := indent + valueSty.Render(value)

	var thr string
	if p.hasThresh {
		thr = styThreshLn.Render("thr " + formatThreshold(p.chart, p.threshold))
	}

	statsLine := lr(indent+styDim.Render(p.statsText(d, stats)), p.failText(stats), w)
	memLine := p.memLine(w, indent)

	switch {
	case ih >= 4:
		lines := []string{
			lr(head, axis, w),
			lr(valueLine, thr, w),
		}
		// With two charts on the row the memory column needs its own value and
		// axis; at exactly four lines it takes the avg/max summary's place
		// rather than the detail line's, which is where errors are reported.
		if memLine == "" || ih >= 5 {
			lines = append(lines, statsLine)
		}
		if memLine != "" {
			lines = append(lines, memLine)
		}
		return append(lines, p.detailLines(w, indent, ih-len(lines))...)
	case ih == 3:
		lines := []string{
			lr(head, axis, w),
			lr(valueLine, thr, w),
		}
		// Three lines is a choice between the memory numbers and the detail
		// line; a failing service says so there, and that always wins.
		if memLine != "" && p.lastErr == "" {
			return append(lines, memLine)
		}
		return append(lines, p.detailLines(w, indent, 1)...)
	case ih == 2:
		return append([]string{
			lr(head, valueSty.Render(value), w),
		}, p.detailLines(w, indent, 1)...)
	default:
		return []string{lr(head, valueSty.Render(value), w)}
	}
}

// headText is the status dot plus the metric title.
func (p panel) headText(d descriptor, w int) string {
	dot := styFaint.Render("○")
	switch {
	case p.alerting():
		dot = styAlert.Render("●")
	case p.series.Len() > 0:
		dot = lipgloss.NewStyle().Foreground(d.fg).Render("●")
	}
	sty := lipgloss.NewStyle().Foreground(colText).Bold(true)
	if p.alerting() {
		sty = styAlertB
	}
	return dot + " " + sty.Render(truncate(p.title, w-2))
}

func (p panel) statsText(d descriptor, stats metrics.Stats) string {
	if stats.OKCount == 0 {
		return "no samples yet"
	}
	return fmt.Sprintf("avg %s · max %s", d.unit.FormatAxis(stats.Avg), d.unit.FormatAxis(stats.Max))
}

func (p panel) failText(stats metrics.Stats) string {
	if stats.FailCount == 0 {
		return ""
	}
	return styAlert.Render(fmt.Sprintf("✕%d", stats.FailCount))
}

// detailLines renders the context line, wrapped over the lines still available.
// A failing check shows its error here instead.
func (p panel) detailLines(w int, indent string, n int) []string {
	if n <= 0 {
		return nil
	}
	text, sty := p.detail, styDim
	if p.lastErr != "" {
		text, sty = "✕ "+p.lastErr, styAlert
	}
	if text == "" {
		text, sty = "waiting…", styFaint
	}

	out := make([]string, 0, n)
	for _, line := range wrapTokens(text, " · ", w-width(indent), n) {
		out = append(out, padStyled(indent+sty.Render(line), w))
	}
	for len(out) < n {
		out = append(out, strings.Repeat(" ", w))
	}
	return out
}

// chartColumns renders the row's chart area: one timeline, or — on a service
// row wide enough to split — CPU and memory side by side, divided by the same
// faint rule that separates the information block from the charts.
func (p panel) chartColumns(d descriptor, max float64) []string {
	rows := p.chartAt(d, p.chartW, p.chartH, max)
	if !p.split() {
		return rows
	}

	md, memMax := p.memAxis()
	mem := chart{
		samples: p.memSeries.Tail(p.memW),
		width:   p.memW,
		height:  p.chartH,
		max:     memMax,
		fg:      md.fg,
		// The threshold belongs to the CPU column: a service alerts on the CPU
		// share it is configured with, and painting the memory chart red for it
		// would claim a breach that was never measured.
	}.render()

	// The gutter repeats the rule the information block uses, so the row reads
	// as three columns rather than as one chart with a gap in it.
	gutter := " " + styFaint.Render("│") + " "
	out := make([]string, len(rows))
	for i := range rows {
		out[i] = rows[i] + gutter
		if i < len(mem) {
			out[i] += mem[i]
		}
	}
	return out
}

// memAxis is the memory column's presentation and the scale it is drawn at.
func (p panel) memAxis() (descriptor, float64) {
	d := serviceMemDescriptor()
	return d, axisMax(d, p.memSeries.Stats(p.memW).Max)
}

// memLine reports the memory column's newest value and its axis maximum, so the
// second chart can be read as numbers and not only as a shape.
func (p panel) memLine(w int, indent string) string {
	if !p.split() {
		return ""
	}
	d, memMax := p.memAxis()
	value, sty := "—", styDim
	if last, ok := p.memSeries.Last(); ok && last.OK {
		value, sty = d.unit.Format(last.Value), styText
	}
	label := lipgloss.NewStyle().Foreground(d.fg).Render("mem")
	return lr(indent+label+" "+sty.Render(value),
		styFaint.Render("⌃"+d.unit.FormatAxis(memMax)), w)
}

// chartAt renders the timeline at an explicit width and height.
func (p panel) chartAt(d descriptor, w, h int, max float64) []string {
	return chart{
		samples:   p.series.Tail(w),
		width:     w,
		height:    h,
		max:       max,
		threshold: p.thresholdOrZero(),
		fg:        d.fg,
		alert:     p.alerting(),
	}.render()
}

func (p panel) thresholdOrZero() float64 {
	if p.hasThresh {
		return p.threshold
	}
	return 0
}

// valueText renders the newest reading and the style it should carry.
func (p panel) valueText(d descriptor) (string, lipgloss.Style) {
	last, ok := p.series.Last()
	if !ok {
		return "—", styDim
	}
	if !last.OK {
		return "FAIL", styAlertB
	}
	if p.alerting() {
		return d.unit.Format(last.Value), styAlertB
	}
	return d.unit.Format(last.Value), lipgloss.NewStyle().Foreground(colText).Bold(true)
}
