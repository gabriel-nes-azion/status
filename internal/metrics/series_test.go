package metrics

import "testing"

func TestSeriesRingBuffer(t *testing.T) {
	s := NewSeries(3)
	if s.Len() != 0 {
		t.Fatalf("new series is not empty")
	}
	if _, ok := s.Last(); ok {
		t.Error("empty series reported a last sample")
	}
	for i := 1; i <= 5; i++ {
		s.Append(Sample{Value: float64(i), OK: true})
	}
	if s.Len() != 3 {
		t.Errorf("len = %d, want 3", s.Len())
	}
	last, _ := s.Last()
	if last.Value != 5 {
		t.Errorf("last = %v, want 5", last.Value)
	}
	tail := s.Tail(10) // asking for more than exists must not pad or panic
	if len(tail) != 3 {
		t.Fatalf("tail len = %d, want 3", len(tail))
	}
	for i, want := range []float64{3, 4, 5} {
		if tail[i].Value != want {
			t.Errorf("tail[%d] = %v, want %v (oldest first)", i, tail[i].Value, want)
		}
	}
}

func TestSeriesResizeKeepsRecent(t *testing.T) {
	s := NewSeries(5)
	for i := 1; i <= 5; i++ {
		s.Append(Sample{Value: float64(i), OK: true})
	}
	s.Resize(2)
	tail := s.Tail(5)
	if len(tail) != 2 || tail[0].Value != 4 || tail[1].Value != 5 {
		t.Errorf("after shrink: %+v, want the two newest", tail)
	}
	s.Resize(4)
	s.Append(Sample{Value: 6, OK: true})
	tail = s.Tail(10)
	if len(tail) != 3 || tail[2].Value != 6 {
		t.Errorf("after grow: %+v", tail)
	}
}

func TestSeriesStatsIgnoresFailures(t *testing.T) {
	s := NewSeries(10)
	s.Append(Sample{Value: 10, OK: true})
	s.Append(Sample{Value: 100, OK: false}) // value must be ignored entirely
	s.Append(Sample{Value: 30, OK: true})

	st := s.Stats(10)
	if st.OKCount != 2 || st.FailCount != 1 {
		t.Errorf("counts = ok %d fail %d, want 2 and 1", st.OKCount, st.FailCount)
	}
	if st.Min != 10 || st.Max != 30 || st.Avg != 20 {
		t.Errorf("min/max/avg = %v/%v/%v, want 10/30/20", st.Min, st.Max, st.Avg)
	}
}

func TestSeriesResetKeepsCapacity(t *testing.T) {
	s := NewSeries(4)
	for i := 0; i < 4; i++ {
		s.Append(Sample{OK: true})
	}
	s.Reset()
	if s.Len() != 0 {
		t.Errorf("len after reset = %d", s.Len())
	}
	for i := 0; i < 6; i++ {
		s.Append(Sample{OK: true})
	}
	if s.Len() != 4 {
		t.Errorf("capacity changed: len = %d, want 4", s.Len())
	}
}
