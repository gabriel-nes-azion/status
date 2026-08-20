package metrics

import (
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	cases := map[float64]string{
		0: "0B", 512: "512B", 1024: "1.0K", 1536: "1.5K",
		10 * 1024: "10K", 1024 * 1024: "1.0M", 100 * 1024 * 1024: "100M",
		1024 * 1024 * 1024: "1.0G",
	}
	for in, want := range cases {
		if got := FormatBytes(in); got != want {
			t.Errorf("FormatBytes(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatMillis(t *testing.T) {
	cases := map[float64]string{
		0.5: "0.50ms", 9.9: "9.90ms", 12.34: "12.3ms",
		150: "150ms", 1500: "1.50s", 12000: "12.0s",
	}
	for in, want := range cases {
		if got := FormatMillis(in); got != want {
			t.Errorf("FormatMillis(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestNiceCeil(t *testing.T) {
	for _, c := range []struct{ in, want float64 }{
		{0, 1}, {0.4, 0.4}, {73.6, 80}, {1035, 1200}, {96, 100},
	} {
		if got := NiceCeil(c.in); got != c.want {
			t.Errorf("NiceCeil(%v) = %v, want %v", c.in, got, c.want)
		}
	}
	// The axis must never crop the data it is scaling.
	for _, v := range []float64{1, 7, 99, 12345, 0.003} {
		if got := NiceCeil(v); got < v {
			t.Errorf("NiceCeil(%v) = %v, which would clip the series", v, got)
		}
	}
}

func TestNiceCeilBinaryUsesBinaryPrefixes(t *testing.T) {
	// A decimal rounding here produces the odd-looking 143M/s axis label.
	if got := NiceCeilBinary(125 << 20); got != 128<<20 {
		t.Errorf("NiceCeilBinary(125M) = %v, want 128M", got)
	}
	if got := FormatRate(NiceCeilBinary(3.2 * (1 << 20))); got != "4.0M/s" {
		t.Errorf("axis label = %q, want 4.0M/s", got)
	}
	for _, v := range []float64{1, 1023, 1 << 20, 900 << 20} {
		if got := NiceCeilBinary(v); got < v {
			t.Errorf("NiceCeilBinary(%v) = %v, which would clip the series", v, got)
		}
	}
}

func TestMillis(t *testing.T) {
	if got := Millis(1500 * time.Microsecond); got != 1.5 {
		t.Errorf("Millis = %v, want 1.5", got)
	}
}
