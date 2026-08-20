package ui

import (
	"fmt"
	"strings"

	"status/internal/config"
)

// overlays are rendered as a framed block covering the chart grid until the
// next keypress. They exist so multi-line answers (/help, /config) do not have
// to be squeezed into the single-line feedback area.

func kv(key, value string) string { return kvw(key, value, 16) }

// kvw is kv with an explicit key column, for lists whose keys are long enough
// that a fixed width would run the two together.
func kvw(key, value string, w int) string {
	return styDim.Render(pad(key, w)) + styText.Render(value)
}

func section(title string) string {
	return styAccentB.Render(strings.ToUpper(title))
}

func helpOverlay() []string {
	// Size the signature column to the longest command rather than a guess, so
	// nothing is silently truncated when a command grows an argument.
	sigW := 0
	for _, c := range commands {
		if n := width(c.signature()); n > sigW {
			sigW = n
		}
	}
	sigW += 2

	lines := []string{section("commands")}
	for _, c := range commands {
		lines = append(lines, "  "+styAccent.Render(pad(c.signature(), sigW))+styDim.Render(c.desc))
	}

	// Keys go two to a line: the overlay has to fit on screen alongside twenty
	// commands, and these are short enough to pair up.
	keys := [][2]string{
		{"shift+tab", "switch between the main and services screens"},
		{"tab", "accept the highlighted completion"},
		{"enter", "run the command, or complete it first"},
		{"up / down", "move through completions, or command history"},
		{"esc", "dismiss this panel, or clear the prompt"},
		{"ctrl+r", "sample everything immediately"},
		{"ctrl+l", "clear chart history"},
		{"ctrl+c", "quit"},
	}
	lines = append(lines, "", section("keys"))
	const keyW, descW = 11, 44
	for i := 0; i < len(keys); i += 2 {
		line := "  " + styAccent.Render(pad(keys[i][0], keyW)) + styDim.Render(pad(keys[i][1], descW))
		if i+1 < len(keys) {
			line += "  " + styAccent.Render(pad(keys[i+1][0], keyW)) + styDim.Render(keys[i+1][1])
		}
		lines = append(lines, line)
	}

	return append(lines,
		"",
		section("configuration"),
		"  "+styDim.Render("everything set here is written on exit and read back next time."),
		"  "+styDim.Render("/config shows the file in use; run with --conf <file> to pick another."),
		"",
		section("reading it"),
		"  "+styDim.Render("two screens: MACHINE and NETWORK on the main one, SERVICES on the second."),
		"  "+styDim.Render("every chart keeps collecting on both, so switching never loses history."),
		"  "+styDim.Render("one row per chart: the numbers up to column 30, the timeline after it."),
		"  "+styDim.Render("time flows left to right; the rightmost column is the newest sample."),
		"  "+styThreshLn.Render("╌")+styDim.Render(" marks the threshold. columns at or above it turn bright red, and the"),
		"  "+styDim.Render("whole row reddens while the newest sample is in breach. a threshold above"),
		"  "+styDim.Render("the visible range still shows as ")+styThreshLn.Render("thr")+styDim.Render(" but draws no line."),
		"  "+styAlert.Render("░")+styDim.Render(" marks a failed check, ")+styFaint.Render("·")+styDim.Render(" marks a column with no data yet."),
	)
}

func configOverlay(cfg config.Settings, paused bool, path string) []string {
	state := styOK.Render("running")
	if paused {
		state = styPaused.Render("paused")
	}
	iface := cfg.Iface
	if iface == "" {
		iface = "all"
	}
	lines := []string{
		section("target"),
		"  " + kv("url", cfg.URL()),
		"  " + kv("ping mode", string(cfg.PingMode)),
		"  " + kv("tls verify", onOff(!cfg.InsecureTLS)),
		"",
		section("sampling"),
		"  " + kv("checks", cfg.Interval.String()),
		"  " + kv("system", cfg.SysInterval.String()),
		"  " + kv("history", fmt.Sprintf("%d samples", cfg.History)),
		"  " + kv("state", state),
		"",
		section("sources"),
		"  " + kv("mountpoint", cfg.Mount),
		"  " + kv("interface", iface),
		"",
		section("charts"),
		"  " + kv("main screen", screenVisibility(cfg, config.ScreenMain)),
		"  " + kv("services", screenVisibility(cfg, config.ScreenServices)),
		"  " + kv("service alert", formatThreshold(config.ServiceChart(""), cfg.ServiceThreshold)+" cpu by default"),
		"",
		section("timeouts"),
	}
	lines = append(lines, timeoutLines(cfg)...)
	lines = append(lines, "", section("thresholds"))
	lines = append(lines, thresholdLines(cfg)...)
	if path == "" {
		path = "(nowhere — no writable config directory)"
	}
	lines = append(lines, "", styFaint.Render("  config file: "+path),
		styFaint.Render("  written on exit and by /save"))
	return lines
}

func timeoutLines(cfg config.Settings) []string {
	out := make([]string, 0, len(config.Probes))
	for _, m := range config.Probes {
		out = append(out, "  "+kv(string(m), cfg.Timeout(m).String()))
	}
	return out
}

func thresholdLines(cfg config.Settings) []string {
	charts := cfg.AllCharts()

	// Service ids are as long as the service name, so the column is sized to
	// the content rather than guessed at.
	keyW := 16
	for _, c := range charts {
		if n := width(string(c)) + 2; n > keyW {
			keyW = n
		}
	}

	out := make([]string, 0, len(charts))
	for _, c := range charts {
		name := string(c)
		v, ok := cfg.Threshold(c)
		if !ok {
			out = append(out, "  "+styDim.Render(pad(name, keyW))+styFaint.Render("disabled"))
			continue
		}
		out = append(out, "  "+kvw(name, formatThreshold(c, v), keyW))
	}
	return out
}

// servicesOverlay lists the charted services and how each one is matched.
func servicesOverlay(cfg config.Settings) []string {
	lines := []string{section("services"), ""}
	if len(cfg.Services) == 0 {
		return append(lines,
			"  "+styDim.Render("nothing charted yet."),
			"",
			"  "+styFaint.Render("/discover           scan the machine and pick from the list"),
			"  "+styFaint.Render("/service add <name> chart a process group by hand"),
		)
	}
	for _, svc := range cfg.Services {
		id := config.ServiceChart(svc.Name)
		state := styOK.Render("shown")
		if !cfg.IsShown(id) {
			state = styFaint.Render("hidden")
		}
		where := "name"
		if svc.Cmdline {
			where = "name or cmdline"
		}
		thr := styFaint.Render("no threshold")
		if v, ok := cfg.Threshold(id); ok {
			thr = styThreshLn.Render("thr " + formatThreshold(id, v))
		}
		lines = append(lines, "  "+styDim.Render(pad(svc.Name, 18))+
			styText.Render(pad("match "+svc.Match, 26))+
			styDim.Render(pad(where, 17))+pad(thr, 20)+state)
	}
	return append(lines, "",
		styFaint.Render("  /service rm <name> to stop charting one · shift+tab for the services screen"))
}

func timeoutsOverlay(cfg config.Settings) []string {
	lines := []string{section("timeouts"), ""}
	lines = append(lines, timeoutLines(cfg)...)
	lines = append(lines, "", styFaint.Render("  /timeout <dns|ping|ttfb|request|all> <duration>"))
	return lines
}

func thresholdsOverlay(cfg config.Settings) []string {
	lines := []string{section("thresholds"), ""}
	lines = append(lines, thresholdLines(cfg)...)
	lines = append(lines, "", styFaint.Render("  /threshold <metric> <value>   ·   0 disables the alert"))
	return lines
}

// screenVisibility summarises which of a screen's charts /show has switched off.
func screenVisibility(cfg config.Settings, screen config.Screen) string {
	var total int
	var hidden []string
	for _, sec := range cfg.AllSections(screen) {
		for _, c := range sec.Charts {
			total++
			if !cfg.IsShown(c) {
				hidden = append(hidden, string(c))
			}
		}
	}
	switch {
	case total == 0:
		return "none configured"
	case len(hidden) == 0:
		return fmt.Sprintf("all %d", total)
	}
	return fmt.Sprintf("%d of %d · hidden: %s", total-len(hidden), total, strings.Join(hidden, ", "))
}
