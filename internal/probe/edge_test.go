package probe

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"status/internal/config"
)

func TestEdgeFromResponsePicksNewestRule(t *testing.T) {
	resp := &http.Response{StatusCode: 404, Header: http.Header{}}
	resp.Header.Set("X-Azion-Edge-Location", "IAD")
	resp.Header.Set("X-Azion-Edge-Pop", "EQN")
	resp.Header.Set("X-Ea-Rule-Last-Modified", "2026-02-08 03:16:15.570902+00:00")
	resp.Header.Set("X-Ef-Rule-Last-Modified", "2026-01-19 20:42:50.459507+00:00")

	e := edgeFromResponse(resp, time.Now())
	if !e.OK || e.Status != 404 {
		t.Errorf("ok=%v status=%d, want a 404 that still came from an edge", e.OK, e.Status)
	}
	if got := e.LocPop(); got != "IAD-EQN" {
		t.Errorf("LocPop = %q, want IAD-EQN", got)
	}
	want := time.Date(2026, 2, 8, 3, 16, 15, 570902000, time.UTC)
	if !e.RuleModified.Equal(want) {
		t.Errorf("RuleModified = %s, want %s", e.RuleModified, want)
	}
}

func TestEdgeFromResponseWithoutDebugHeaders(t *testing.T) {
	resp := &http.Response{StatusCode: 200, Header: http.Header{}}
	resp.Header.Set("X-Ef-Rule-Last-Modified", "not a timestamp")
	e := edgeFromResponse(resp, time.Now())
	if e.LocPop() != "" || !e.RuleModified.IsZero() {
		t.Errorf("got locpop %q and rule %s from a response without debug headers", e.LocPop(), e.RuleModified)
	}
}

func TestHTTPSendsDebugPragma(t *testing.T) {
	var pragma string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pragma = r.Header.Get("Pragma")
		w.Header().Set("X-Azion-Edge-Location", "GRU")
		w.Header().Set("X-Azion-Edge-Pop", "SPO")
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	t2, err := config.ParseTarget(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultSettings()
	cfg.Scheme, cfg.Host, cfg.Path = t2.Scheme, t2.Host, t2.Path

	_, _, edge := New().HTTP(t.Context(), cfg)
	if pragma != edgeDebugPragma {
		t.Errorf("Pragma = %q, want %q", pragma, edgeDebugPragma)
	}
	if !edge.OK || edge.Status != http.StatusTeapot || edge.LocPop() != "GRU-SPO" {
		t.Errorf("edge = %+v", edge)
	}
}
