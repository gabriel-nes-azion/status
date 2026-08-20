package metrics

import (
	"fmt"
	"math"
	"time"
)

// Unit determines how a value is rendered and how the chart axis is scaled.
type Unit int

const (
	UnitPercent Unit = iota
	UnitBytesPerSec
	UnitMillis
)

// Format renders v in its unit, using a compact fixed-ish width.
func (u Unit) Format(v float64) string {
	switch u {
	case UnitPercent:
		return fmt.Sprintf("%.1f%%", v)
	case UnitBytesPerSec:
		return FormatRate(v)
	case UnitMillis:
		return FormatMillis(v)
	}
	return fmt.Sprintf("%.2f", v)
}

// FormatAxis renders the top-of-chart scale label, which needs fewer digits.
func (u Unit) FormatAxis(v float64) string {
	switch u {
	case UnitPercent:
		return fmt.Sprintf("%.0f%%", v)
	case UnitBytesPerSec:
		return FormatRate(v)
	case UnitMillis:
		if v >= 1000 {
			return fmt.Sprintf("%.1fs", v/1000)
		}
		return fmt.Sprintf("%.0fms", v)
	}
	return fmt.Sprintf("%.0f", v)
}

// FormatMillis renders a millisecond duration with adaptive precision.
func FormatMillis(ms float64) string {
	switch {
	case ms >= 10000:
		return fmt.Sprintf("%.1fs", ms/1000)
	case ms >= 1000:
		return fmt.Sprintf("%.2fs", ms/1000)
	case ms >= 100:
		return fmt.Sprintf("%.0fms", ms)
	case ms >= 10:
		return fmt.Sprintf("%.1fms", ms)
	default:
		return fmt.Sprintf("%.2fms", ms)
	}
}

// FormatBytes renders a byte count with binary prefixes.
func FormatBytes(b float64) string {
	const unit = 1024.0
	if b < unit {
		return fmt.Sprintf("%.0fB", b)
	}
	units := []string{"K", "M", "G", "T", "P"}
	v := b
	for _, suffix := range units {
		v /= unit
		if v < unit {
			if v < 10 {
				return fmt.Sprintf("%.1f%s", v, suffix)
			}
			return fmt.Sprintf("%.0f%s", v, suffix)
		}
	}
	return fmt.Sprintf("%.0fP", v)
}

// FormatRate renders a bytes-per-second rate.
func FormatRate(bps float64) string { return FormatBytes(bps) + "/s" }

// Millis converts a duration to a float millisecond value.
func Millis(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// NiceCeilBinary rounds v up to a pleasant axis maximum in powers of 1024, so
// that a byte-rate axis is labelled 128M/s rather than the 143M/s a decimal
// rounding would produce.
func NiceCeilBinary(v float64) float64 {
	const unit = 1024.0
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return unit
	}
	pow := 1.0
	for v/pow >= unit {
		pow *= unit
	}
	mant := v / pow
	for _, step := range []float64{1, 2, 4, 8, 16, 32, 64, 128, 256, 512} {
		if mant <= step+1e-9 {
			return step * pow
		}
	}
	return unit * pow
}

// NiceCeil rounds v up to a visually pleasant axis maximum (1, 2, 2.5 or 5
// times a power of ten) so the chart scale does not jitter on every sample.
func NiceCeil(v float64) float64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 1
	}
	exp := math.Floor(math.Log10(v))
	pow := math.Pow(10, exp)
	frac := v / pow
	for _, step := range []float64{1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10} {
		if frac <= step+1e-9 {
			return step * pow
		}
	}
	return 10 * pow
}
