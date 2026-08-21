package metrics

import "time"

// Sample is one observation. OK is false when the underlying check failed, in
// which case Value is meaningless and the chart renders an outage bar.
type Sample struct {
	At    time.Time
	Value float64
	OK    bool
}

// Series is a fixed-capacity ring buffer of samples ordered oldest to newest.
// It is only touched from the UI goroutine, so it needs no locking.
type Series struct {
	buf  []Sample
	next int
	n    int
}

func NewSeries(capacity int) *Series {
	if capacity < 1 {
		capacity = 1
	}
	return &Series{buf: make([]Sample, capacity)}
}

func (s *Series) Append(sm Sample) {
	s.buf[s.next] = sm
	s.next = (s.next + 1) % len(s.buf)
	if s.n < len(s.buf) {
		s.n++
	}
}

// The read methods tolerate a nil receiver, so a chart whose series has not been
// created yet renders as "no data" instead of taking the whole dashboard down
// with it. Writing to a nil series still panics: that is a real bug, and it
// should be loud.

func (s *Series) Len() int {
	if s == nil {
		return 0
	}
	return s.n
}

// Reset drops all samples but keeps the capacity.
func (s *Series) Reset() {
	s.next = 0
	s.n = 0
}

// Resize changes the capacity, keeping the most recent samples that fit.
func (s *Series) Resize(capacity int) {
	if capacity < 1 {
		capacity = 1
	}
	if capacity == len(s.buf) {
		return
	}
	keep := s.Tail(capacity)
	s.buf = make([]Sample, capacity)
	s.next = 0
	s.n = 0
	for _, sm := range keep {
		s.Append(sm)
	}
}

// Tail returns up to n most recent samples, oldest first.
func (s *Series) Tail(n int) []Sample {
	if s == nil {
		return nil
	}
	if n > s.n {
		n = s.n
	}
	if n <= 0 {
		return nil
	}
	out := make([]Sample, 0, n)
	start := (s.next - n + len(s.buf)) % len(s.buf)
	for i := 0; i < n; i++ {
		out = append(out, s.buf[(start+i)%len(s.buf)])
	}
	return out
}

// Last returns the newest sample.
func (s *Series) Last() (Sample, bool) {
	if s == nil || s.n == 0 {
		return Sample{}, false
	}
	idx := (s.next - 1 + len(s.buf)) % len(s.buf)
	return s.buf[idx], true
}

// Stats summarises the successful samples in the n most recent entries.
type Stats struct {
	Min, Max, Avg float64
	OKCount       int
	FailCount     int
}

func (s *Series) Stats(n int) Stats {
	if s == nil {
		return Stats{}
	}
	tail := s.Tail(n)
	st := Stats{}
	var sum float64
	for _, sm := range tail {
		if !sm.OK {
			st.FailCount++
			continue
		}
		if st.OKCount == 0 || sm.Value < st.Min {
			st.Min = sm.Value
		}
		if st.OKCount == 0 || sm.Value > st.Max {
			st.Max = sm.Value
		}
		sum += sm.Value
		st.OKCount++
	}
	if st.OKCount > 0 {
		st.Avg = sum / float64(st.OKCount)
	}
	return st
}
