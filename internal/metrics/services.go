package metrics

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	psnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// ServiceSpec is what the collector needs to find a service's processes.
type ServiceSpec struct {
	Name    string
	Match   string
	Cmdline bool
}

// ServiceSample is one service's resource use over the last interval.
type ServiceSample struct {
	Name string
	// CPUPercent is the share of the whole machine's CPU capacity, matching the
	// CPU panel's scale rather than top's percent-of-one-core.
	CPUPercent float64
	Cores      float64 // the same figure expressed in cores, for the detail line
	MemBytes   uint64
	PIDs       int
	Err        error
	// Warmup is true when there is no previous CPU counter to diff against, so
	// the rate is not meaningful yet.
	Warmup bool
}

// serviceState is the previous CPU counter of one service.
type serviceState struct {
	cpuSeconds float64
	at         time.Time
}

// ServiceCollector charts the CPU share of process groups.
//
// Per-process CPU is derived from the cumulative user+system time counters and
// diffed between samples. gopsutil's CPUPercent averages over the process's
// whole lifetime instead, which would draw a flat line through a spike.
type ServiceCollector struct {
	cores int
	prev  map[string]serviceState
}

func NewServiceCollector(cores int) *ServiceCollector {
	if cores < 1 {
		cores = 1
	}
	return &ServiceCollector{cores: cores, prev: map[string]serviceState{}}
}

// Forget drops a service's counter history, so a re-added service does not
// report a huge first delta against a stale baseline.
func (c *ServiceCollector) Forget(name string) { delete(c.prev, name) }

// Collect samples every spec in one pass over the process table. Cmdline is only
// read when a spec asks for it, since it is the expensive field.
func (c *ServiceCollector) Collect(ctx context.Context, specs []ServiceSpec) []ServiceSample {
	if len(specs) == 0 {
		return nil
	}
	now := time.Now()

	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		out := make([]ServiceSample, 0, len(specs))
		for _, spec := range specs {
			out = append(out, ServiceSample{Name: spec.Name, Err: err})
		}
		return out
	}

	wantCmdline := false
	for _, spec := range specs {
		if spec.Cmdline {
			wantCmdline = true
			break
		}
	}

	type totals struct {
		cpuSeconds float64
		mem        uint64
		pids       int
	}
	acc := make(map[string]*totals, len(specs))
	for _, spec := range specs {
		acc[spec.Name] = &totals{}
	}

	for _, p := range procs {
		name, err := p.NameWithContext(ctx)
		if err != nil {
			continue
		}
		lowerName := strings.ToLower(name)

		cmdline := ""
		if wantCmdline {
			// Errors are expected for processes we cannot inspect; an empty
			// command line simply never matches.
			cmdline, _ = p.CmdlineWithContext(ctx)
			cmdline = strings.ToLower(cmdline)
		}

		for _, spec := range specs {
			if !specMatches(spec, lowerName, cmdline) {
				continue
			}
			t := acc[spec.Name]
			t.pids++
			if times, err := p.TimesWithContext(ctx); err == nil {
				t.cpuSeconds += times.User + times.System
			}
			if mi, err := p.MemoryInfoWithContext(ctx); err == nil && mi != nil {
				t.mem += mi.RSS
			}
		}
	}

	out := make([]ServiceSample, 0, len(specs))
	for _, spec := range specs {
		t := acc[spec.Name]
		s := ServiceSample{Name: spec.Name, PIDs: t.pids, MemBytes: t.mem}

		prev, had := c.prev[spec.Name]
		c.prev[spec.Name] = serviceState{cpuSeconds: t.cpuSeconds, at: now}

		switch {
		case !had:
			s.Warmup = true
		default:
			elapsed := now.Sub(prev.at).Seconds()
			delta := t.cpuSeconds - prev.cpuSeconds
			if elapsed > 0 && delta >= 0 {
				s.Cores = delta / elapsed
				s.CPUPercent = s.Cores / float64(c.cores) * 100
				if s.CPUPercent > 100 {
					s.CPUPercent = 100
				}
			}
			// A negative delta means the process set changed under us (a restart,
			// a worker recycled). Report zero rather than a phantom spike.
		}
		if t.pids == 0 {
			s.Err = fmt.Errorf("no process matching %q", spec.Match)
		}
		out = append(out, s)
	}
	return out
}

func specMatches(spec ServiceSpec, lowerName, lowerCmdline string) bool {
	needle := strings.ToLower(spec.Match)
	if needle == "" {
		return false
	}
	if strings.Contains(lowerName, needle) {
		return true
	}
	return spec.Cmdline && lowerCmdline != "" && strings.Contains(lowerCmdline, needle)
}

// Candidate is a service the machine looks like it is running.
type Candidate struct {
	Name       string
	Exe        string
	PIDs       int
	CPUPercent float64 // lifetime average, enough to rank candidates
	MemBytes   uint64
	Ports      []uint32
	// Listening marks a process group holding a listening socket, which is the
	// strongest available signal that something is a service rather than a tool.
	Listening bool
}

// Discover scans the machine for things worth charting: processes grouped by
// name, ranked so that the ones holding listening sockets come first.
//
// Listening sockets are the discriminator. "Which processes are services" has no
// answer the OS will give directly, and neither launchd nor systemd covers the
// containers and dev servers people actually watch, so the heuristic is: a
// process that accepts connections is a service; otherwise rank by CPU.
func Discover(ctx context.Context, cores int, filter string) ([]Candidate, error) {
	if cores < 1 {
		cores = 1
	}
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}

	// Listening sockets are best-effort: on macOS this shells out to lsof, which
	// can be denied or absent. Discovery still works without it.
	listening := map[int32][]uint32{}
	if conns, err := psnet.ConnectionsWithContext(ctx, "inet"); err == nil {
		for _, c := range conns {
			if c.Status != "LISTEN" || c.Pid == 0 {
				continue
			}
			listening[c.Pid] = append(listening[c.Pid], c.Laddr.Port)
		}
	}

	needle := strings.ToLower(strings.TrimSpace(filter))
	byName := map[string]*Candidate{}

	for _, p := range procs {
		name, err := p.NameWithContext(ctx)
		if err != nil || name == "" {
			continue
		}
		ports, isListening := listening[p.Pid]

		if needle != "" && !strings.Contains(strings.ToLower(name), needle) {
			// A filtered-out name can still match on its command line, which is
			// how you find "the python that serves the API".
			cmd, _ := p.CmdlineWithContext(ctx)
			if !strings.Contains(strings.ToLower(cmd), needle) {
				continue
			}
		}

		c := byName[name]
		if c == nil {
			c = &Candidate{Name: name}
			if exe, err := p.ExeWithContext(ctx); err == nil {
				c.Exe = exe
			}
			byName[name] = c
		}
		c.PIDs++
		if pct, err := p.CPUPercentWithContext(ctx); err == nil {
			c.CPUPercent += pct / float64(cores)
		}
		if mi, err := p.MemoryInfoWithContext(ctx); err == nil && mi != nil {
			c.MemBytes += mi.RSS
		}
		if isListening {
			c.Listening = true
			c.Ports = append(c.Ports, ports...)
		}
	}

	out := make([]Candidate, 0, len(byName))
	for _, c := range byName {
		c.Ports = dedupePorts(c.Ports)
		out = append(out, *c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Listening != out[j].Listening {
			return out[i].Listening
		}
		if out[i].CPUPercent != out[j].CPUPercent {
			return out[i].CPUPercent > out[j].CPUPercent
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func dedupePorts(ports []uint32) []uint32 {
	if len(ports) == 0 {
		return nil
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	out := ports[:1]
	for _, p := range ports[1:] {
		if p != out[len(out)-1] {
			out = append(out, p)
		}
	}
	return out
}

// FormatPorts renders a candidate's listening ports for the discovery list.
func FormatPorts(ports []uint32) string {
	if len(ports) == 0 {
		return ""
	}
	if len(ports) > 3 {
		ports = ports[:3]
	}
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%d", p))
	}
	return ":" + strings.Join(parts, ",")
}

// SelfName is this process's own name, which tests use as a match guaranteed to
// hit at least one running process.
func SelfName() (string, error) {
	p, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		return "", err
	}
	return p.Name()
}
