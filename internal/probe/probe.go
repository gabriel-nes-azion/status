// Package probe implements the periodic network checks: DNS resolution time,
// ICMP/TCP latency, time to first byte and full request time.
package probe

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"status/internal/config"
)

// Result is one completed check. Value is always in milliseconds so every
// latency panel shares a unit.
type Result struct {
	Metric config.Metric
	At     time.Time
	Value  float64
	OK     bool
	Detail string
	Err    error
}

func fail(m config.Metric, err error) Result {
	return Result{Metric: m, At: time.Now(), OK: false, Err: err, Detail: shortErr(err)}
}

func ok(m config.Metric, d time.Duration, detail string) Result {
	return Result{
		Metric: m,
		At:     time.Now(),
		Value:  float64(d) / float64(time.Millisecond),
		OK:     true,
		Detail: detail,
	}
}

// Prober owns the small amount of state the checks need between runs, notably
// whether unprivileged ICMP works on this host.
type Prober struct {
	icmpBroken atomic.Bool
}

func New() *Prober { return &Prober{} }

// ResetCapabilities re-enables ICMP probing, used when /pingmode changes.
func (p *Prober) ResetCapabilities() { p.icmpBroken.Store(false) }

// shortErr trims the noisy wrapping Go adds to network errors so the message
// fits on a single panel line.
func shortErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Net errors wrap the whole operation ("Get \"https://…\": dial tcp …:
	// connection refused"); only the last clause fits on a panel line.
	for _, prefix := range []string{"Get \"", "lookup ", "dial tcp ", "read tcp "} {
		if !strings.HasPrefix(msg, prefix) {
			continue
		}
		if j := strings.LastIndex(msg, ": "); j > 0 {
			msg = msg[j+2:]
		}
		break
	}
	if strings.Contains(msg, "context deadline exceeded") {
		return "timeout"
	}
	if len(msg) > 60 {
		msg = msg[:57] + "..."
	}
	return msg
}

// Target is a normalised check destination.
type Target struct {
	Scheme string
	Host   string
	Path   string
}

// ParseTarget accepts a bare hostname, a host:port pair or a full URL and
// normalises it into scheme/host/path.
func ParseTarget(input string) (Target, error) {
	in := strings.TrimSpace(input)
	if in == "" {
		return Target{}, fmt.Errorf("empty host")
	}
	if !strings.Contains(in, "://") {
		// Bare host, optionally with a path: "example.com/health".
		in = "https://" + in
	}
	u, err := url.Parse(in)
	if err != nil {
		return Target{}, fmt.Errorf("invalid host: %w", err)
	}
	if u.Hostname() == "" {
		return Target{}, fmt.Errorf("invalid host: no hostname in %q", input)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Target{}, fmt.Errorf("unsupported scheme %q (use http or https)", u.Scheme)
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	return Target{Scheme: u.Scheme, Host: u.Host, Path: path}, nil
}

// hostname strips any port from cfg.Host, for DNS and ICMP which are
// port-agnostic.
func hostname(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}

// port returns the TCP port implied by the target, used by the TCP ping mode.
func port(cfg config.Settings) string {
	if _, p, err := net.SplitHostPort(cfg.Host); err == nil {
		return p
	}
	if cfg.Scheme == "http" {
		return "80"
	}
	return "443"
}
