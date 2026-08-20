package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The TOML file is written by hand rather than through an encoder so that it can
// carry the comments that make it editable: nothing in `diskio = 104857600`
// tells you the unit, and a config file meant to be opened in an editor should
// say. Reading goes through a real parser (see load); only writing is ours.

// encode renders the settings as a commented TOML document.
func encode(s Settings, path string) string {
	var b strings.Builder

	b.WriteString("# status — terminal monitoring dashboard\n")
	b.WriteString("#\n")
	b.WriteString("# Written on exit and by /save, and read back on start. Safe to edit by hand:\n")
	b.WriteString("# anything missing falls back to the built-in default.\n")
	b.WriteString("#\n")
	b.WriteString("#   status --conf " + pathHint(path) + "\n")

	b.WriteString("\n[target]\n")
	b.WriteString(kvString("url", s.URL()))
	b.WriteString(comment(kvString("ping_mode", string(s.PingMode)), "icmp, tcp or auto"))
	b.WriteString(comment(kvBool("insecure_tls", s.InsecureTLS), "skip TLS verification"))

	b.WriteString("\n[sampling]\n")
	b.WriteString(comment(kvString("checks", s.Interval.String()), "network check interval"))
	b.WriteString(comment(kvString("system", s.SysInterval.String()), "local metric interval"))
	b.WriteString(comment(kvInt("history", s.History), "samples kept per chart"))

	b.WriteString("\n[sources]\n")
	b.WriteString(comment(kvString("mount", s.Mount), "filesystem for the disk usage chart"))
	b.WriteString(comment(kvString("interface", s.Iface), "network interface, empty for all"))

	b.WriteString("\n# Per-check timeouts.\n[timeouts]\n")
	for _, m := range Probes {
		b.WriteString(kvString(string(m), s.Timeout(m).String()))
	}

	b.WriteString("\n# Alert levels: the chart turns red at or above these. 0 disables one.\n")
	b.WriteString("# cpu/mem/disk are percent, diskio/net are bytes per second,\n")
	b.WriteString("# dns/ping/ttfb/request are milliseconds, services are percent of total CPU.\n")
	b.WriteString("[thresholds]\n")
	for _, c := range s.AllCharts() {
		v, ok := s.Thresholds[c]
		if !ok {
			continue
		}
		b.WriteString(kvFloat(string(c), v))
	}
	// A threshold for a chart that no longer exists is kept rather than dropped:
	// removing a service should not silently forget the level set for it.
	for _, c := range orphanKeys(s.Thresholds, s.AllCharts()) {
		b.WriteString(kvFloat(string(c), s.Thresholds[c]))
	}
	b.WriteString(comment(kvFloat("service_default", s.ServiceThreshold), "applied to a newly added service"))

	b.WriteString("\n# Which charts are displayed. Every chart is listed; a chart missing from\n")
	b.WriteString("# here is shown, so a new one never arrives hidden.\n")
	b.WriteString("[charts]\n")
	for _, c := range s.AllCharts() {
		b.WriteString(kvBool(string(c), s.IsShown(c)))
	}

	if len(s.Services) == 0 {
		b.WriteString("\n# Services to chart. /discover fills this in; each entry looks like:\n")
		b.WriteString("#\n")
		b.WriteString("#   [[services]]\n")
		b.WriteString("#   name = \"nginx\"\n")
		b.WriteString("#   match = \"nginx\"    # case-insensitive substring of the process name\n")
		b.WriteString("#   cmdline = false    # also match against the full command line\n")
		return b.String()
	}
	b.WriteString("\n# Services to chart. match is a case-insensitive substring of the process\n")
	b.WriteString("# name; cmdline also tests it against the full command line.\n")
	for _, svc := range s.Services {
		b.WriteString("\n[[services]]\n")
		b.WriteString(kvString("name", svc.Name))
		b.WriteString(kvString("match", svc.Match))
		if svc.Cmdline {
			b.WriteString(kvBool("cmdline", true))
		}
	}
	return b.String()
}

// pathHint is the example path shown in the file header.
func pathHint(path string) string {
	if path != "" {
		return path
	}
	return "<file>.toml"
}

// orphanKeys are threshold keys with no matching chart, in stable order.
func orphanKeys(m map[ChartID]float64, live []ChartID) []ChartID {
	known := make(map[ChartID]bool, len(live))
	for _, c := range live {
		known[c] = true
	}
	var out []ChartID
	for c := range m {
		if !known[c] {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func comment(line, note string) string {
	return strings.TrimRight(line, "\n") + "  # " + note + "\n"
}

func kvString(key, v string) string { return tomlKey(key) + " = " + quote(v) + "\n" }
func kvBool(key string, v bool) string {
	return tomlKey(key) + " = " + strconv.FormatBool(v) + "\n"
}
func kvInt(key string, v int) string {
	return tomlKey(key) + " = " + strconv.Itoa(v) + "\n"
}

// kvFloat writes a float TOML can read back as a float: TOML has no implicit
// int-to-float conversion, so a whole number still needs its decimal point.
func kvFloat(key string, v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return tomlKey(key) + " = " + s + "\n"
}

// tomlKey renders a table key, quoting it when it is not a bare key. Chart ids
// contain a colon ("service:nginx"), which a bare key may not.
func tomlKey(k string) string {
	if k == "" {
		return `""`
	}
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return quote(k)
		}
	}
	return k
}

// quote renders a TOML basic string. Only the escapes TOML actually defines are
// used; anything else non-printable goes out as \uXXXX.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			switch {
			case r == utf8.RuneError:
				// Invalid UTF-8 would make the file unparseable.
				b.WriteString(`�`)
			case r < 0x20 || r == 0x7f:
				b.WriteString(fmt.Sprintf(`\u%04X`, r))
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
