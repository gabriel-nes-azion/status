package ui

import (
	"strings"
	"testing"
	"time"

	"status/internal/config"
	"status/internal/probe"
)

func edgeAt(at time.Time, rule time.Time) probe.Edge {
	return probe.Edge{At: at, OK: true, Status: 200, Location: "IAD", PoP: "EQN", RuleModified: rule}
}

func TestEdgeOrchestrationLatency(t *testing.T) {
	m := newSized(t, 140, 44)
	c := config.MetricChart(config.Edge)
	now := time.Now().UTC()

	m.applyEdge(edgeAt(now, now.Add(-30*24*time.Hour)))
	if m.hasOrch {
		t.Fatal("the first rule timestamp is a baseline, not a propagation time")
	}
	if last, _ := m.series[c].Last(); last.Value != 0 || last.Label != "IAD-EQN" || last.Code != 200 {
		t.Errorf("first sample = %+v", last)
	}

	m.applyEdge(edgeAt(now.Add(5*time.Second), now.Add(-30*24*time.Hour)))
	if m.hasOrch {
		t.Error("an unchanged rule timestamp is not an update")
	}

	rule := now.Add(-4 * time.Second)
	m.applyEdge(edgeAt(now.Add(10*time.Second), rule))
	if !m.hasOrch || m.edgeOrch != 14000 {
		t.Errorf("orch = %v (set %v), want 14000ms", m.edgeOrch, m.hasOrch)
	}
	if last, _ := m.series[c].Last(); last.Value != 14000 {
		t.Errorf("update sample value = %v, want 14000", last.Value)
	}

	// A lagging edge still reporting the previous rules is not a new change.
	m.applyEdge(edgeAt(now.Add(15*time.Second), now.Add(-30*24*time.Hour)))
	if last, _ := m.series[c].Last(); last.Value != 0 {
		t.Errorf("an older rule timestamp was counted as an update: %+v", last)
	}
	if m.edgeOrch != 14000 {
		t.Errorf("orch = %v, want the last update kept", m.edgeOrch)
	}

	m.resetProbeSeries()
	if m.hasOrch || !m.edgeRule.IsZero() || m.series[c].Len() != 0 {
		t.Error("changing the target must restart the baseline")
	}
}

func TestEdgeFailureIsAnOutage(t *testing.T) {
	m := newSized(t, 140, 44)
	c := config.MetricChart(config.Edge)
	m.applyEdge(probe.Edge{At: time.Now(), Detail: "timeout"})
	if last, _ := m.series[c].Last(); last.OK {
		t.Error("a request with no response should record a failed sample")
	}
	if m.errmsg[c] != "timeout" {
		t.Errorf("errmsg = %q", m.errmsg[c])
	}
}

func TestEdgeRowRenders(t *testing.T) {
	for _, h := range []int{17, 24, 44, 80} {
		m := newSized(t, 120, h)
		seed(m, 200)
		now := time.Now().UTC()
		base := now.Add(-time.Hour)
		for i := 0; i < 60; i++ {
			e := edgeAt(now.Add(time.Duration(i)*time.Second), base)
			if i >= 30 {
				e.Location, e.PoP, e.Status = "GRU", "SPO", 404
			}
			if i == 40 {
				e.RuleModified = now.Add(time.Duration(i-3) * time.Second)
			}
			m.applyEdge(e)
		}
		out := m.View()
		if !strings.Contains(out, "EDGE") {
			t.Fatalf("h=%d: no EDGE row", h)
		}
		if !strings.Contains(out, "orch 3.0s") {
			t.Errorf("h=%d: orch latency not shown", h)
		}
		if got := len(strings.Split(out, "\n")); got > h {
			t.Errorf("h=%d: view is %d lines", h, got)
		}
		if h >= 44 && !strings.Contains(out, "GRU-SPO") {
			t.Errorf("h=%d: loc-pop change not labelled on the timeline", h)
		}
		if h == 80 {
			t.Log("\n" + out)
		}
	}
}

func TestFormatOrch(t *testing.T) {
	for ms, want := range map[float64]string{
		3000: "3.0s", 59500: "59.5s", 125000: "2m5s", -2000: "-2.0s",
	} {
		if got := formatOrch(ms); got != want {
			t.Errorf("formatOrch(%v) = %q, want %q", ms, got, want)
		}
	}
}
