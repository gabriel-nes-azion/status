package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"status/internal/config"
	"status/internal/metrics"
)

// parseDuration accepts a Go duration ("500ms", "2s", "1m") or a bare number,
// which is interpreted as seconds.
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("missing duration")
	}
	if d, err := time.ParseDuration(s); err == nil {
		if d <= 0 {
			return 0, fmt.Errorf("duration must be positive")
		}
		return d, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (try 500ms, 2s, 1m)", s)
	}
	if f <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}
	return time.Duration(f * float64(time.Second)), nil
}

// parseRate accepts byte-rate values such as "100MB/s", "12.5M", "1GiB" or a
// bare byte count.
func parseRate(s string) (float64, error) {
	in := strings.ToLower(strings.TrimSpace(s))
	in = strings.TrimSuffix(in, "/s")
	in = strings.TrimSuffix(in, "ps")
	in = strings.TrimSuffix(in, "b")
	in = strings.TrimSuffix(in, "i") // the "i" of KiB, after "b" is stripped
	in = strings.TrimSpace(in)

	mult := 1.0
	if in != "" {
		switch in[len(in)-1] {
		case 'k':
			mult = 1 << 10
		case 'm':
			mult = 1 << 20
		case 'g':
			mult = 1 << 30
		case 't':
			mult = 1 << 40
		}
		if mult > 1 {
			in = strings.TrimSpace(in[:len(in)-1])
		}
	}
	f, err := strconv.ParseFloat(in, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid rate %q (try 100MB/s, 512K, 1G)", s)
	}
	if f < 0 {
		return 0, fmt.Errorf("rate must not be negative")
	}
	return f * mult, nil
}

// parseThreshold interprets a threshold value in the unit of its chart.
func parseThreshold(c config.ChartID, s string) (float64, error) {
	switch descriptorFor(c).unit {
	case metrics.UnitPercent:
		v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid percentage %q", s)
		}
		if v < 0 || v > 100 {
			return 0, fmt.Errorf("percentage must be between 0 and 100")
		}
		return v, nil
	case metrics.UnitBytesPerSec:
		return parseRate(s)
	default: // milliseconds
		if d, err := time.ParseDuration(strings.TrimSpace(s)); err == nil {
			return metrics.Millis(d), nil
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid latency %q (try 300, 300ms, 1.5s)", s)
		}
		if v < 0 {
			return 0, fmt.Errorf("latency must not be negative")
		}
		return v, nil
	}
}

// formatThreshold renders a threshold in its chart's unit.
func formatThreshold(c config.ChartID, v float64) string {
	return descriptorFor(c).unit.Format(v)
}

// parseMetric resolves a user-supplied metric name.
func parseMetric(s string) (config.Metric, error) {
	want := strings.ToLower(strings.TrimSpace(s))
	for _, m := range config.Order {
		if string(m) == want {
			return m, nil
		}
	}
	return "", fmt.Errorf("unknown metric %q (one of: %s)", s, joinMetrics(config.Order))
}

func joinMetrics(ms []config.Metric) string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = string(m)
	}
	return strings.Join(out, ", ")
}

// parseOnOff reads a boolean toggle, where an empty value means "flip it".
func parseOnOff(s string, current bool) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return !current, nil
	case "on", "true", "yes", "1", "enable":
		return true, nil
	case "off", "false", "no", "0", "disable":
		return false, nil
	}
	return current, fmt.Errorf("expected on or off, got %q", s)
}

// splitArgs tokenises a prompt line, honouring single and double quotes so an
// argument can contain spaces — a service match is often a whole command line.
// An unterminated quote is treated as running to the end of the line, which is
// what a user mid-typing means.
func splitArgs(s string) []string {
	var (
		out   []string
		cur   strings.Builder
		quote rune
		open  bool
	)
	flush := func() {
		if open || cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
		open = false
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
			open = true // an empty "" is still an argument
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}
