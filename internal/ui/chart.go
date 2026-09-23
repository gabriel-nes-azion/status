package ui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"status/internal/metrics"
)

// blocks maps an eighth-of-a-cell fill level to a partial block glyph.
var blocks = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

const (
	outageGlyph    = '░' // a check that failed outright
	emptyGlyph     = ' '
	thresholdGlyph = '╌'
	noDataGlyph    = '·'
	// thresholdStride draws the guide line as a sparse dash so it reads as a
	// reference mark instead of competing with the data.
	thresholdStride = 3
)

// chart renders a time-series area chart. Time flows left to right, so the
// newest sample is the rightmost column; when there is less history than there
// are columns the series is right-aligned and the gap on the left is marked as
// "no data" rather than as zero.
type chart struct {
	samples   []metrics.Sample
	width     int
	height    int
	max       float64
	threshold float64 // 0 disables the guide line and breach colouring
	fg        lipgloss.Color
	// alert turns the whole series red, not just the columns above the
	// threshold, so a breaching panel is unmistakable at a glance while the
	// brighter breach columns still show when it started.
	alert bool
}

// cell is the resolved state of one chart column.
type column struct {
	level   float64 // 0..1 of the axis
	failed  bool
	breach  bool
	present bool
}

// render returns exactly height lines, each width cells wide.
func (c chart) render() []string {
	if c.width < 1 || c.height < 1 {
		return nil
	}
	cols := c.columns()

	normal := lipgloss.NewStyle().Foreground(c.fg)
	if c.alert {
		normal = styAlertDim
	}
	thresholdRow := c.thresholdRow()

	out := make([]string, 0, c.height)
	for row := 0; row < c.height; row++ {
		out = append(out, c.renderRow(cols, row, row == thresholdRow, normal))
	}
	return out
}

// columns resolves the sample window into one entry per output column.
func (c chart) columns() []column {
	cols := make([]column, c.width)
	tail := c.samples
	if len(tail) > c.width {
		tail = tail[len(tail)-c.width:]
	}
	offset := c.width - len(tail) // right-align the series
	for i, s := range tail {
		col := column{present: true, failed: !s.OK}
		if s.OK {
			if c.max > 0 {
				col.level = s.Value / c.max
			}
			if col.level > 1 {
				col.level = 1
			}
			if col.level < 0 {
				col.level = 0
			}
			col.breach = c.threshold > 0 && s.Value >= c.threshold
		}
		cols[offset+i] = col
	}
	return cols
}

// thresholdRow is the row index the guide line is drawn on, or -1.
func (c chart) thresholdRow() int {
	if c.threshold <= 0 || c.max <= 0 || c.threshold > c.max {
		return -1
	}
	filled := c.threshold / c.max * float64(c.height)
	row := int(math.Round(float64(c.height) - filled))
	if row < 0 {
		row = 0
	}
	if row >= c.height {
		row = c.height - 1
	}
	return row
}

// renderRow builds one line, grouping runs of same-styled cells into a single
// styled span so a full frame stays cheap to write.
func (c chart) renderRow(cols []column, row int, isThreshold bool, normal lipgloss.Style) string {
	var (
		b      strings.Builder
		run    strings.Builder
		runSty *lipgloss.Style
		flush  = func() {
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
		emit = func(r rune, sty *lipgloss.Style) {
			if sty != runSty {
				flush()
				runSty = sty
			}
			run.WriteRune(r)
		}
	)

	for i, col := range cols {
		dash := isThreshold && i%thresholdStride == 0
		switch {
		case !col.present:
			if dash {
				emit(thresholdGlyph, &styFaint)
			} else if row == c.height-1 {
				emit(noDataGlyph, &styFaint)
			} else {
				emit(emptyGlyph, nil)
			}
		case col.failed:
			emit(outageGlyph, &styAlert)
		default:
			glyph := c.glyph(col.level, row)
			if glyph == blocks[0] {
				if dash {
					emit(thresholdGlyph, &styThreshLn)
				} else {
					emit(emptyGlyph, nil)
				}
				continue
			}
			if col.breach {
				emit(glyph, &styAlert)
			} else {
				emit(glyph, &normal)
			}
		}
	}
	flush()
	return b.String()
}

// glyph picks the block for a column of the given fill level at the given row,
// where row 0 is the top of the chart.
func (c chart) glyph(level float64, row int) rune {
	filled := level * float64(c.height)
	remaining := filled - float64(c.height-1-row)
	switch {
	case remaining >= 1:
		return blocks[8]
	case remaining <= 0:
		return blocks[0]
	}
	idx := int(math.Round(remaining * 8))
	if idx < 1 {
		// Keep a non-zero sample visible instead of rounding it away.
		idx = 1
	}
	if idx > 8 {
		idx = 8
	}
	return blocks[idx]
}

// axisMax derives the chart's vertical scale from the data actually in the
// window, rounded to a "nice" number so the axis does not jitter with every new
// sample. The threshold deliberately does not stretch the axis: a 100MB/s limit
// would otherwise flatten normal traffic into an unreadable line. When a
// threshold sits above the visible range its guide line is simply not drawn —
// by the time it matters, the breach itself has raised the scale.
func axisMax(d descriptor, observed float64) float64 {
	if d.fixedMax > 0 {
		return d.fixedMax
	}
	max := observed * 1.15
	if d.floorMax > max {
		max = d.floorMax
	}
	if d.unit == metrics.UnitBytesPerSec || d.unit == metrics.UnitBytes {
		return metrics.NiceCeilBinary(max)
	}
	return metrics.NiceCeil(max)
}
