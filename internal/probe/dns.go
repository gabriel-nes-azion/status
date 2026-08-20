package probe

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"status/internal/config"
)

// goResolver bypasses the platform resolver library so timings reflect an
// actual query on the wire instead of an OS-level cache hit.
var goResolver = &net.Resolver{PreferGo: true}

// DNS measures how long it takes to resolve the target hostname. It prefers
// Go's own resolver (uncached, so the number is a real query) and falls back to
// the system resolver when /etc/resolv.conf is unusable.
func (p *Prober) DNS(ctx context.Context, cfg config.Settings) Result {
	host := hostname(cfg.Host)
	if ip := net.ParseIP(host); ip != nil {
		return Result{
			Metric: config.DNS,
			At:     time.Now(),
			Value:  0,
			OK:     true,
			Detail: "literal IP · no lookup",
		}
	}

	timeout := cfg.Timeout(config.DNS)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	addrs, err := goResolver.LookupHost(ctx, host)
	elapsed := time.Since(start)
	via := "pure-go"

	if err != nil {
		// The pure-Go resolver cannot work without a usable resolv.conf; retry
		// through the platform resolver before calling the check failed.
		start = time.Now()
		addrs, err = net.DefaultResolver.LookupHost(ctx, host)
		elapsed = time.Since(start)
		via = "system"
	}
	if err != nil {
		return fail(config.DNS, err)
	}
	return ok(config.DNS, elapsed, fmt.Sprintf("%s · %d addr · %s", via, len(addrs), firstAddrs(addrs, 2)))
}

func firstAddrs(addrs []string, n int) string {
	if len(addrs) > n {
		addrs = addrs[:n]
	}
	return strings.Join(addrs, " · ")
}
