package probe

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"time"

	"status/internal/config"

	"status/internal/metrics"
)

const userAgent = "status-tui/1.0 (+terminal monitor)"

// HTTP performs one request against the target and returns the TTFB, the full
// request-time and the edge results. A single request feeds all three panels so
// they always describe the same transaction, and keep-alive is disabled so
// every check pays a fresh DNS/TCP/TLS cost and the samples stay comparable.
//
// The two configured timeouts map onto distinct phases: the TTFB timeout bounds
// the wait for response headers, the request timeout bounds the whole exchange
// including the body.
func (p *Prober) HTTP(ctx context.Context, cfg config.Settings) (ttfb Result, total Result, edge Edge) {
	reqTimeout := cfg.Timeout(config.Request)
	ttfbTimeout := cfg.Timeout(config.TTFB)

	ctx, cancel := context.WithTimeout(ctx, reqTimeout)
	defer cancel()

	var (
		dnsStart, connStart, tlsStart time.Time
		dnsDur, connDur, tlsDur       time.Duration
		firstByte                     time.Duration
		reused                        bool
		remote                        string
	)

	start := time.Now()
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone:  func(httptrace.DNSDoneInfo) { dnsDur = time.Since(dnsStart) },
		ConnectStart: func(string, string) {
			if connStart.IsZero() {
				connStart = time.Now()
			}
		},
		ConnectDone: func(_, addr string, err error) {
			if err == nil {
				connDur = time.Since(connStart)
				remote = addr
			}
		},
		TLSHandshakeStart: func() { tlsStart = time.Now() },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err == nil {
				tlsDur = time.Since(tlsStart)
			}
		},
		GotConn:              func(i httptrace.GotConnInfo) { reused = i.Reused },
		GotFirstResponseByte: func() { firstByte = time.Since(start) },
	}

	transport := &http.Transport{
		DisableKeepAlives:     true,
		DisableCompression:    false,
		ResponseHeaderTimeout: ttfbTimeout,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: cfg.InsecureTLS}, //nolint:gosec // opt-in via /insecure
		DialContext:           (&net.Dialer{Timeout: ttfbTimeout}).DialContext,
		TLSHandshakeTimeout:   ttfbTimeout,
	}
	defer transport.CloseIdleConnections()

	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, cfg.URL(), nil)
	if err != nil {
		return fail(config.TTFB, err), fail(config.Request, err), edgeFail(err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", edgeDebugPragma)

	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		return fail(config.TTFB, err), fail(config.Request, err), edgeFail(err)
	}
	defer resp.Body.Close()
	edge = edgeFromResponse(resp, time.Now())

	n, readErr := io.Copy(io.Discard, resp.Body)
	elapsed := time.Since(start)

	phases := fmt.Sprintf("dns %s · conn %s", metrics.FormatMillis(metrics.Millis(dnsDur)), metrics.FormatMillis(metrics.Millis(connDur)))
	if tlsDur > 0 {
		phases += " · tls " + metrics.FormatMillis(metrics.Millis(tlsDur))
	}
	if reused {
		phases += " · reused"
	}
	ttfb = ok(config.TTFB, firstByte, phases)

	if readErr != nil {
		total = fail(config.Request, readErr)
		return ttfb, total, edge
	}

	detail := fmt.Sprintf("%d · %s · %s", resp.StatusCode, resp.Proto, metrics.FormatBytes(float64(n)))
	if remote != "" {
		detail += " · " + remote
	}
	total = ok(config.Request, elapsed, detail)

	// A response of 400 or above is a failed check: the endpoint answered, but
	// not with what was asked for. Both panels mark it as an outage rather than
	// plotting a latency that would read as healthy.
	if resp.StatusCode >= 400 {
		total.OK = false
		total.Err = fmt.Errorf("http %d", resp.StatusCode)
		ttfb.OK = false
		ttfb.Err = total.Err
	}
	return ttfb, total, edge
}
