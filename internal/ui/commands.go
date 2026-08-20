package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"status/internal/config"
	"status/internal/metrics"
	"status/internal/probe"
)

// command is one slash command. run returns the feedback line shown under the
// prompt; suggest supplies completions for the argument at argIndex (0-based).
type command struct {
	name    string
	args    string
	desc    string
	run     func(m *Model, args []string) (string, tea.Cmd, error)
	suggest func(m *Model, argIndex int, prefix string) []string
}

// signature renders "/cmd <args>" for the completion list.
func (c command) signature() string {
	if c.args == "" {
		return c.name
	}
	return c.name + " " + c.args
}

var commands []command

// commandByName looks up an exact command name.
func commandByName(name string) (command, bool) {
	for _, c := range commands {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

// matchCommands returns every command whose name starts with prefix.
func matchCommands(prefix string) []command {
	prefix = strings.ToLower(prefix)
	var out []command
	for _, c := range commands {
		if strings.HasPrefix(c.name, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func filterPrefix(candidates []string, prefix string) []string {
	prefix = strings.ToLower(prefix)
	var out []string
	for _, c := range candidates {
		if strings.HasPrefix(strings.ToLower(c), prefix) {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

func metricNames() []string {
	out := make([]string, 0, len(config.Order))
	for _, m := range config.Order {
		out = append(out, string(m))
	}
	return out
}

func probeNames() []string {
	out := []string{"all"}
	for _, m := range config.Probes {
		out = append(out, string(m))
	}
	return out
}

func init() {
	commands = []command{
		{
			name: "/help", desc: "list every command and keybinding",
			run: func(m *Model, _ []string) (string, tea.Cmd, error) {
				m.overlay = helpOverlay()
				m.overlayTitle = "HELP"
				return "", nil, nil
			},
		},
		{
			name: "/host", args: "<domain|url>", desc: "change the target of the network checks",
			run: func(m *Model, args []string) (string, tea.Cmd, error) {
				if len(args) == 0 {
					return "", nil, fmt.Errorf("usage: /host <domain|url>")
				}
				t, err := probe.ParseTarget(args[0])
				if err != nil {
					return "", nil, err
				}
				m.cfg.Update(func(c *config.Settings) {
					c.Scheme, c.Host, c.Path = t.Scheme, t.Host, t.Path
				})
				m.prober.ResetCapabilities()
				m.resetProbeSeries()
				return fmt.Sprintf("target set to %s", m.cfg.Snapshot().URL()), m.runChecks(), nil
			},
		},
		{
			name: "/interval", args: "<duration>", desc: "how often the network checks run (default 5s)",
			suggest: func(_ *Model, i int, p string) []string {
				if i != 0 {
					return nil
				}
				return filterPrefix([]string{"1s", "2s", "5s", "10s", "30s", "1m"}, p)
			},
			run: func(m *Model, args []string) (string, tea.Cmd, error) {
				if len(args) == 0 {
					return "", nil, fmt.Errorf("usage: /interval <duration>")
				}
				d, err := parseDuration(args[0])
				if err != nil {
					return "", nil, err
				}
				m.cfg.Update(func(c *config.Settings) { c.Interval = d })
				return fmt.Sprintf("check interval set to %s", d), m.scheduleChecks(), nil
			},
		},
		{
			name: "/sysinterval", args: "<duration>", desc: "how often local system metrics are sampled (default 1s)",
			suggest: func(_ *Model, i int, p string) []string {
				if i != 0 {
					return nil
				}
				return filterPrefix([]string{"250ms", "500ms", "1s", "2s", "5s"}, p)
			},
			run: func(m *Model, args []string) (string, tea.Cmd, error) {
				if len(args) == 0 {
					return "", nil, fmt.Errorf("usage: /sysinterval <duration>")
				}
				d, err := parseDuration(args[0])
				if err != nil {
					return "", nil, err
				}
				m.cfg.Update(func(c *config.Settings) { c.SysInterval = d })
				return fmt.Sprintf("system sampling set to %s", d), m.scheduleSystem(), nil
			},
		},
		{
			name: "/timeout", args: "<dns|ping|ttfb|request|all> <duration>", desc: "set a per-check timeout",
			suggest: func(_ *Model, i int, p string) []string {
				switch i {
				case 0:
					return filterPrefix(probeNames(), p)
				case 1:
					return filterPrefix([]string{"500ms", "1s", "2s", "5s", "10s"}, p)
				}
				return nil
			},
			run: func(m *Model, args []string) (string, tea.Cmd, error) {
				if len(args) < 2 {
					return "", nil, fmt.Errorf("usage: /timeout <%s> <duration>", strings.Join(probeNames(), "|"))
				}
				d, err := parseDuration(args[1])
				if err != nil {
					return "", nil, err
				}
				targets := config.Probes
				if args[0] != "all" {
					mm, err := parseMetric(args[0])
					if err != nil {
						return "", nil, err
					}
					if !isProbe(mm) {
						return "", nil, fmt.Errorf("%s is a local metric and has no timeout", mm)
					}
					targets = []config.Metric{mm}
				}
				m.cfg.Update(func(c *config.Settings) {
					for _, t := range targets {
						c.Timeouts[t] = d
					}
				})
				return fmt.Sprintf("timeout for %s set to %s", joinMetrics(targets), d), nil, nil
			},
		},
		{
			name: "/timeouts", desc: "show every configured timeout",
			run: func(m *Model, _ []string) (string, tea.Cmd, error) {
				m.overlay = timeoutsOverlay(m.cfg.Snapshot())
				m.overlayTitle = "TIMEOUTS"
				return "", nil, nil
			},
		},
		{
			name: "/threshold", args: "<chart> <value>", desc: "alert level that turns a chart red (0 disables)",
			suggest: func(m *Model, i int, p string) []string {
				if i == 0 {
					return filterPrefix(m.cfg.Snapshot().ChartNames(), p)
				}
				return nil
			},
			run: func(m *Model, args []string) (string, tea.Cmd, error) {
				snap := m.cfg.Snapshot()
				if len(args) < 2 {
					return "", nil, fmt.Errorf("usage: /threshold <%s> <value>",
						strings.Join(snap.ChartNames(), "|"))
				}
				c, err := snap.ResolveChart(args[0])
				if err != nil {
					return "", nil, err
				}
				v, err := parseThreshold(c, args[1])
				if err != nil {
					return "", nil, err
				}
				m.cfg.Update(func(s *config.Settings) { s.Thresholds[c] = v })
				name := snap.ChartTitle(c)
				if v == 0 {
					return fmt.Sprintf("threshold for %s disabled", name), nil, nil
				}
				return fmt.Sprintf("threshold for %s set to %s", name, formatThreshold(c, v)), nil, nil
			},
		},
		{
			name: "/thresholds", desc: "show every configured threshold",
			run: func(m *Model, _ []string) (string, tea.Cmd, error) {
				m.overlay = thresholdsOverlay(m.cfg.Snapshot())
				m.overlayTitle = "THRESHOLDS"
				return "", nil, nil
			},
		},
		{
			name: "/show", args: "[chart|section|all|none]", desc: "pick which charts are displayed",
			suggest: func(m *Model, i int, p string) []string {
				if i != 0 {
					return nil
				}
				opts := append([]string{"all", "none"}, sectionNames()...)
				return filterPrefix(append(opts, m.cfg.Snapshot().ChartNames()...), p)
			},
			run: func(m *Model, args []string) (string, tea.Cmd, error) {
				snap := m.cfg.Snapshot()
				if len(args) == 0 {
					m.picker = true
					m.pickerIdx = 0
					return "", nil, nil
				}
				arg := strings.ToLower(args[0])
				switch arg {
				case "all":
					m.setShown(snap.AllCharts(), true)
					return "showing all charts", nil, nil
				case "none":
					m.setShown(snap.AllCharts(), false)
					return "hid all charts — /show all to restore", nil, nil
				}
				// A section name toggles the whole group: hide it when every
				// member is visible, otherwise bring the group back.
				for _, screen := range config.Screens {
					for _, sec := range snap.AllSections(screen) {
						if !strings.EqualFold(sec.Name, arg) {
							continue
						}
						if len(sec.Charts) == 0 {
							return "", nil, fmt.Errorf("the %s section is empty — try /discover", sec.Name)
						}
						allVisible := true
						for _, c := range sec.Charts {
							if !snap.IsShown(c) {
								allVisible = false
								break
							}
						}
						m.setShown(sec.Charts, !allVisible)
						return fmt.Sprintf("%s charts %s", sec.Name, shownHidden(!allVisible)), nil, nil
					}
				}
				c, err := snap.ResolveChart(arg)
				if err != nil {
					return "", nil, fmt.Errorf("%w — expected a chart, a section (%s), all or none",
						err, strings.Join(sectionNames(), ", "))
				}
				m.toggleShown(c)
				return fmt.Sprintf("%s %s", snap.ChartTitle(c),
					shownHidden(m.cfg.Snapshot().IsShown(c))), nil, nil
			},
		},
		{
			name: "/screen", args: "[main|services]", desc: "switch screens (shift+tab cycles)",
			suggest: func(_ *Model, i int, p string) []string {
				if i != 0 {
					return nil
				}
				return filterPrefix([]string{"main", "services"}, p)
			},
			run: func(m *Model, args []string) (string, tea.Cmd, error) {
				if len(args) == 0 {
					m.screen = m.screen.Next()
					return "showing the " + m.screen.String() + " screen", nil, nil
				}
				sc, err := config.ParseScreen(args[0])
				if err != nil {
					return "", nil, err
				}
				m.screen = sc
				return "showing the " + sc.String() + " screen", nil, nil
			},
		},
		{
			name: "/discover", args: "[filter]", desc: "scan the machine for services to chart",
			run: func(m *Model, args []string) (string, tea.Cmd, error) {
				if m.scanning {
					return "", nil, fmt.Errorf("a scan is already running")
				}
				filter := ""
				if len(args) > 0 {
					filter = args[0]
				}
				m.scanning = true
				msg := "scanning processes…"
				if filter != "" {
					msg = "scanning processes matching " + filter + "…"
				}
				return msg, m.discover(filter), nil
			},
		},
		{
			name: "/service", args: "<add|rm|list> [name] [match]", desc: "manage the charted services by hand",
			suggest: func(m *Model, i int, p string) []string {
				switch i {
				case 0:
					return filterPrefix([]string{"add", "rm", "list"}, p)
				case 1:
					return filterPrefix(serviceNames(m), p)
				}
				return nil
			},
			run: func(m *Model, args []string) (string, tea.Cmd, error) {
				if len(args) == 0 {
					return "", nil, fmt.Errorf("usage: /service <add|rm|list> [name] [match]")
				}
				switch strings.ToLower(args[0]) {
				case "list":
					m.overlay = servicesOverlay(m.cfg.Snapshot())
					m.overlayTitle = "SERVICES"
					return "", nil, nil

				case "add":
					if len(args) < 2 {
						return "", nil, fmt.Errorf("usage: /service add <name> [match]")
					}
					name := config.NormaliseServiceName(args[1])
					if name == "" {
						return "", nil, fmt.Errorf("invalid service name %q", args[1])
					}
					match := name
					cmdline := false
					if len(args) > 2 {
						match = strings.Join(args[2:], " ")
						// A match with a space or a path separator can only come
						// from a command line, so look there too.
						cmdline = strings.ContainsAny(match, " /")
					}
					m.cfg.Update(func(s *config.Settings) {
						s.AddService(config.Service{Name: name, Match: match, Cmdline: cmdline})
					})
					m.svcColl.Forget(name)
					m.syncSeries(m.cfg.Snapshot())
					return fmt.Sprintf("monitoring %s (match %q)", name, match), m.collectServices(), nil

				case "rm":
					if len(args) < 2 {
						return "", nil, fmt.Errorf("usage: /service rm <name>")
					}
					name := config.NormaliseServiceName(args[1])
					removed := false
					m.cfg.Update(func(s *config.Settings) { removed = s.RemoveService(name) })
					if !removed {
						return "", nil, fmt.Errorf("no service called %q", args[1])
					}
					m.svcColl.Forget(name)
					m.syncSeries(m.cfg.Snapshot())
					return "stopped monitoring " + name, nil, nil
				}
				return "", nil, fmt.Errorf("unknown subcommand %q (add, rm or list)", args[0])
			},
		},
		{
			name: "/disk", args: "<mountpoint>", desc: "which filesystem the disk usage panel reports",
			suggest: func(_ *Model, i int, p string) []string {
				if i != 0 {
					return nil
				}
				return filterPrefix(metrics.Mounts(), p)
			},
			run: func(m *Model, args []string) (string, tea.Cmd, error) {
				if len(args) == 0 {
					return "", nil, fmt.Errorf("usage: /disk <mountpoint>")
				}
				m.cfg.Update(func(c *config.Settings) { c.Mount = args[0] })
				m.series[config.MetricChart(config.Disk)].Reset()
				return fmt.Sprintf("disk usage now reporting %s", args[0]), nil, nil
			},
		},
		{
			name: "/net", args: "<interface|all>", desc: "which interface the network panel sums",
			suggest: func(_ *Model, i int, p string) []string {
				if i != 0 {
					return nil
				}
				return filterPrefix(append([]string{"all"}, metrics.Interfaces()...), p)
			},
			run: func(m *Model, args []string) (string, tea.Cmd, error) {
				if len(args) == 0 {
					return "", nil, fmt.Errorf("usage: /net <interface|all>")
				}
				iface := args[0]
				if iface == "all" {
					iface = ""
				}
				m.cfg.Update(func(c *config.Settings) { c.Iface = iface })
				m.series[config.MetricChart(config.Net)].Reset()
				return fmt.Sprintf("network panel now reporting %s", args[0]), nil, nil
			},
		},
		{
			name: "/pingmode", args: "<auto|icmp|tcp>", desc: "latency transport: ICMP echo or TCP handshake",
			suggest: func(_ *Model, i int, p string) []string {
				if i != 0 {
					return nil
				}
				return filterPrefix([]string{"auto", "icmp", "tcp"}, p)
			},
			run: func(m *Model, args []string) (string, tea.Cmd, error) {
				if len(args) == 0 {
					return "", nil, fmt.Errorf("usage: /pingmode <auto|icmp|tcp>")
				}
				mode := config.PingMode(strings.ToLower(args[0]))
				switch mode {
				case config.PingAuto, config.PingICMP, config.PingTCP:
				default:
					return "", nil, fmt.Errorf("unknown ping mode %q", args[0])
				}
				m.cfg.Update(func(c *config.Settings) { c.PingMode = mode })
				m.prober.ResetCapabilities()
				m.series[config.MetricChart(config.Ping)].Reset()
				return fmt.Sprintf("ping mode set to %s", mode), nil, nil
			},
		},
		{
			name: "/history", args: "<samples>", desc: "how many samples to keep per series (default 600)",
			run: func(m *Model, args []string) (string, tea.Cmd, error) {
				if len(args) == 0 {
					return "", nil, fmt.Errorf("usage: /history <samples>")
				}
				var n int
				if _, err := fmt.Sscanf(args[0], "%d", &n); err != nil || n < 10 {
					return "", nil, fmt.Errorf("history must be an integer >= 10")
				}
				m.cfg.Update(func(c *config.Settings) { c.History = n })
				for _, s := range m.series {
					s.Resize(n)
				}
				return fmt.Sprintf("history set to %d samples per series", n), nil, nil
			},
		},
		{
			name: "/insecure", args: "[on|off]", desc: "skip TLS certificate verification on HTTP checks",
			suggest: func(_ *Model, i int, p string) []string {
				if i != 0 {
					return nil
				}
				return filterPrefix([]string{"on", "off"}, p)
			},
			run: func(m *Model, args []string) (string, tea.Cmd, error) {
				arg := ""
				if len(args) > 0 {
					arg = args[0]
				}
				v, err := parseOnOff(arg, m.cfg.Snapshot().InsecureTLS)
				if err != nil {
					return "", nil, err
				}
				m.cfg.Update(func(c *config.Settings) { c.InsecureTLS = v })
				return fmt.Sprintf("TLS verification %s", onOff(!v)), nil, nil
			},
		},
		{
			name: "/pause", desc: "stop collecting (charts keep the history)",
			run: func(m *Model, _ []string) (string, tea.Cmd, error) {
				m.paused = true
				return "collection paused", nil, nil
			},
		},
		{
			name: "/resume", desc: "resume collecting",
			run: func(m *Model, _ []string) (string, tea.Cmd, error) {
				if !m.paused {
					return "already running", nil, nil
				}
				m.paused = false
				return "collection resumed", tea.Batch(m.scheduleSystem(), m.scheduleChecks(), m.runChecks()), nil
			},
		},
		{
			name: "/clear", desc: "discard all chart history",
			run: func(m *Model, _ []string) (string, tea.Cmd, error) {
				for _, s := range m.series {
					s.Reset()
				}
				return "history cleared", nil, nil
			},
		},
		{
			name: "/config", desc: "show the full current configuration",
			run: func(m *Model, _ []string) (string, tea.Cmd, error) {
				m.overlay = configOverlay(m.cfg.Snapshot(), m.paused)
				m.overlayTitle = "CONFIG"
				return "", nil, nil
			},
		},
		{
			name: "/save", desc: "persist the current configuration to disk",
			run: func(m *Model, _ []string) (string, tea.Cmd, error) {
				path, err := m.cfg.Save()
				if err != nil {
					return "", nil, err
				}
				return "configuration saved to " + path, nil, nil
			},
		},
		{
			name: "/reset", desc: "restore built-in defaults (does not touch the saved file)",
			run: func(m *Model, _ []string) (string, tea.Cmd, error) {
				def := config.DefaultSettings()
				m.cfg.Replace(def)
				m.prober.ResetCapabilities()
				m.syncSeries(def)
				for _, s := range m.series {
					s.Reset()
					s.Resize(def.History)
				}
				return "configuration reset to defaults", tea.Batch(m.scheduleSystem(), m.scheduleChecks(), m.runChecks()), nil
			},
		},
		{
			name: "/quit", desc: "exit",
			run: func(_ *Model, _ []string) (string, tea.Cmd, error) {
				return "", tea.Quit, nil
			},
		},
	}
}

// sectionNames lists every section heading across both screens. The names are
// structural, not derived from what is configured, so an empty Services section
// is still a name /show accepts.
func sectionNames() []string {
	var empty config.Settings
	var out []string
	for _, screen := range config.Screens {
		for _, sec := range empty.AllSections(screen) {
			out = append(out, strings.ToLower(sec.Name))
		}
	}
	return out
}

func serviceNames(m *Model) []string {
	svcs := m.cfg.Snapshot().Services
	out := make([]string, 0, len(svcs))
	for _, s := range svcs {
		out = append(out, s.Name)
	}
	return out
}

func shownHidden(shown bool) string {
	if shown {
		return "shown"
	}
	return "hidden"
}

func isProbe(m config.Metric) bool {
	for _, p := range config.Probes {
		if p == m {
			return true
		}
	}
	return false
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}
