package ui

import (
	"hash/fnv"

	"github.com/charmbracelet/lipgloss"

	"status/internal/config"
	"status/internal/metrics"
)

// Palette. Kept as 256-colour indices so the layout looks the same across
// terminals without depending on a particular colour scheme.
var (
	colText    = lipgloss.Color("252")
	colDim     = lipgloss.Color("243")
	colFaint   = lipgloss.Color("238")
	colAlert   = lipgloss.Color("203")
	colAlertBg = lipgloss.Color("167") // muted red for an alerting chart body
	colOK      = lipgloss.Color("78")
	colAccent  = lipgloss.Color("111")
	colWarnDot = lipgloss.Color("137")
)

var (
	styText     = lipgloss.NewStyle().Foreground(colText)
	styDim      = lipgloss.NewStyle().Foreground(colDim)
	styFaint    = lipgloss.NewStyle().Foreground(colFaint)
	styAlert    = lipgloss.NewStyle().Foreground(colAlert)
	styAlertDim = lipgloss.NewStyle().Foreground(colAlertBg)
	styAlertB   = lipgloss.NewStyle().Foreground(colAlert).Bold(true)
	styOK       = lipgloss.NewStyle().Foreground(colOK)
	styAccent   = lipgloss.NewStyle().Foreground(colAccent)
	styAccentB  = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styThreshLn = lipgloss.NewStyle().Foreground(colWarnDot)
	styPaused   = lipgloss.NewStyle().Foreground(colWarnDot).Bold(true)
)

// descriptor holds the per-chart presentation rules: how values are formatted,
// what colour the chart uses and how the vertical axis is scaled.
type descriptor struct {
	unit metrics.Unit
	fg   lipgloss.Color
	// fixedMax pins the axis maximum (percent charts). Zero means the axis is
	// derived from the observed data.
	fixedMax float64
	// floorMax keeps a dynamic axis from collapsing onto tiny values, which
	// would make idle noise look like a spike.
	floorMax float64
}

var descriptors = map[config.Metric]descriptor{
	config.CPU:     {unit: metrics.UnitPercent, fg: lipgloss.Color("80"), fixedMax: 100},
	config.Mem:     {unit: metrics.UnitPercent, fg: lipgloss.Color("140"), fixedMax: 100},
	config.Disk:    {unit: metrics.UnitPercent, fg: lipgloss.Color("68"), fixedMax: 100},
	config.DiskIO:  {unit: metrics.UnitBytesPerSec, fg: lipgloss.Color("179"), floorMax: 1 << 20},
	config.Net:     {unit: metrics.UnitBytesPerSec, fg: lipgloss.Color("108"), floorMax: 128 << 10},
	config.DNS:     {unit: metrics.UnitMillis, fg: lipgloss.Color("116"), floorMax: 50},
	config.Ping:    {unit: metrics.UnitMillis, fg: lipgloss.Color("146"), floorMax: 50},
	config.TTFB:    {unit: metrics.UnitMillis, fg: lipgloss.Color("215"), floorMax: 100},
	config.Request: {unit: metrics.UnitMillis, fg: lipgloss.Color("211"), floorMax: 200},
}

// servicePalette colours service charts. A service is picked from it by a hash
// of its name so the colour is stable across restarts and independent of the
// order services were added in.
var servicePalette = []lipgloss.Color{
	lipgloss.Color("110"), lipgloss.Color("150"), lipgloss.Color("180"),
	lipgloss.Color("175"), lipgloss.Color("109"), lipgloss.Color("144"),
	lipgloss.Color("173"), lipgloss.Color("115"), lipgloss.Color("141"),
	lipgloss.Color("209"),
}

func descriptorFor(c config.ChartID) descriptor {
	if m, ok := c.Metric(); ok {
		if d, found := descriptors[m]; found {
			return d
		}
	}
	if c.IsService() {
		// Service charts plot CPU share of the whole machine, on the same 0-100
		// scale as the CPU panel so the two can be read against each other.
		return descriptor{unit: metrics.UnitPercent, fg: serviceColor(c.ServiceName()), fixedMax: 100}
	}
	return descriptor{unit: metrics.UnitMillis, fg: colAccent, floorMax: 100}
}

func serviceColor(name string) lipgloss.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return servicePalette[int(h.Sum32())%len(servicePalette)]
}
