package probe

import (
	"context"
	"fmt"
	"net"
	"time"

	probing "github.com/prometheus-community/pro-bing"

	"status/internal/config"
)

// Ping measures round-trip latency to the target host.
//
// ICMP is preferred, using unprivileged datagram sockets so no root is needed
// on macOS and on Linux hosts with net.ipv4.ping_group_range configured. When
// that is unavailable the check falls back to a TCP handshake against the
// target port, which is a coarser but always-available signal. The mode is
// reported in the panel detail line so the number is never ambiguous.
func (p *Prober) Ping(ctx context.Context, cfg config.Settings) Result {
	timeout := cfg.Timeout(config.Ping)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch cfg.PingMode {
	case config.PingTCP:
		return p.tcpPing(ctx, cfg, timeout)
	case config.PingICMP:
		return p.icmpPing(ctx, cfg, timeout)
	default: // auto
		if p.icmpBroken.Load() {
			return p.tcpPing(ctx, cfg, timeout)
		}
		res := p.icmpPing(ctx, cfg, timeout)
		if res.OK {
			return res
		}
		// A timeout means ICMP is probably filtered rather than unsupported;
		// either way TCP is the more useful signal from here on.
		p.icmpBroken.Store(true)
		tcp := p.tcpPing(ctx, cfg, timeout)
		if tcp.OK {
			tcp.Detail = "tcp (icmp unavailable) · " + tcp.Detail
		}
		return tcp
	}
}

func (p *Prober) icmpPing(ctx context.Context, cfg config.Settings, timeout time.Duration) Result {
	pinger, err := probing.NewPinger(hostname(cfg.Host))
	if err != nil {
		return fail(config.Ping, err)
	}
	pinger.Count = 1
	pinger.Timeout = timeout
	pinger.Interval = 10 * time.Millisecond
	pinger.SetPrivileged(false)

	if err := pinger.RunWithContext(ctx); err != nil {
		return fail(config.Ping, err)
	}
	st := pinger.Statistics()
	if st.PacketsRecv == 0 {
		return fail(config.Ping, fmt.Errorf("no icmp reply"))
	}
	return ok(config.Ping, st.AvgRtt, fmt.Sprintf("icmp · %s", st.IPAddr))
}

func (p *Prober) tcpPing(ctx context.Context, cfg config.Settings, timeout time.Duration) Result {
	addr := net.JoinHostPort(hostname(cfg.Host), port(cfg))
	d := net.Dialer{Timeout: timeout}

	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", addr)
	elapsed := time.Since(start)
	if err != nil {
		return fail(config.Ping, err)
	}
	remote := conn.RemoteAddr().String()
	_ = conn.Close()
	return ok(config.Ping, elapsed, fmt.Sprintf("tcp handshake · %s", remote))
}
