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

	rowH   int // total lines in the row, including the chart's blank last line
	infoW  int // width of the information block; the chart starts at this offset
	chartW int
	chartH int
}

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
	rows := p.chartAt(d, p.chartW, p.chartH, max)

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

	switch {
	case ih >= 4:
		lines := []string{
			lr(head, axis, w),
			lr(valueLine, thr, w),
			lr(indent+styDim.Render(p.statsText(d, stats)), p.failText(stats), w),
		}
		return append(lines, p.detailLines(w, indent, ih-len(lines))...)
	case ih == 3:
		return append([]string{
			lr(head, axis, w),
			lr(valueLine, thr, w),
		}, p.detailLines(w, indent, 1)...)
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
