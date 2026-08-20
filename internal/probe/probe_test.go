package probe

import (
	"testing"

	"status/internal/config"
)

func TestHostnameStripsPort(t *testing.T) {
	if got := hostname("example.com:8443"); got != "example.com" {
		t.Errorf("hostname = %q", got)
	}
	if got := hostname("example.com"); got != "example.com" {
		t.Errorf("hostname = %q", got)
	}
}

func TestPortFromTarget(t *testing.T) {
	cases := []struct {
		host, scheme, want string
	}{
		{"example.com", "https", "443"},
		{"example.com", "http", "80"},
		{"example.com:8443", "https", "8443"},
	}
	for _, c := range cases {
		cfg := settings(c.host, c.scheme)
		if got := port(cfg); got != c.want {
			t.Errorf("port(%s, %s) = %q, want %q", c.host, c.scheme, got, c.want)
		}
	}
}

func TestShortErrCollapsesTimeouts(t *testing.T) {
	if got := shortErr(errTimeout{}); got != "timeout" {
		t.Errorf("shortErr = %q, want timeout", got)
	}
	if got := shortErr(nil); got != "" {
		t.Errorf("shortErr(nil) = %q", got)
	}
}

type errTimeout struct{}

func (errTimeout) Error() string {
	return `Get "https://example.com/": context deadline exceeded`
}

func settings(host, scheme string) config.Settings {
	s := config.DefaultSettings()
	s.Host, s.Scheme = host, scheme
	return s
}
