package probe

import (
	"net/http"
	"strings"
	"time"
)

// edgeDebugPragma asks an Azion edge to report which location and PoP answered
// and when its rules were last modified.
const edgeDebugPragma = "azion-debug-cache"

// ruleTimeLayout is the format of the x-e*-rule-last-modified headers, e.g.
// "2026-02-08 03:16:15.570902+00:00".
const ruleTimeLayout = "2006-01-02 15:04:05.999999999-07:00"

// Edge is what the edge serving the HTTP check reported about itself. OK is
// false only when no response arrived at all: a 404 still came from an edge.
type Edge struct {
	At       time.Time
	OK       bool
	Status   int
	Location string
	PoP      string
	// RuleModified is the newer of the edge application and edge firewall rule
	// timestamps, zero when the response carried neither.
	RuleModified time.Time
	Detail       string
}

// LocPop is the location and PoP pair, e.g. "IAD-EQN", empty when the
// response carried neither.
func (e Edge) LocPop() string {
	switch {
	case e.Location != "" && e.PoP != "":
		return e.Location + "-" + e.PoP
	case e.Location != "":
		return e.Location
	}
	return e.PoP
}

func edgeFail(err error) Edge {
	return Edge{At: time.Now(), Detail: shortErr(err)}
}

func edgeFromResponse(resp *http.Response, at time.Time) Edge {
	h := resp.Header
	e := Edge{
		At:       at,
		OK:       true,
		Status:   resp.StatusCode,
		Location: strings.TrimSpace(h.Get("X-Azion-Edge-Location")),
		PoP:      strings.TrimSpace(h.Get("X-Azion-Edge-Pop")),
	}
	for _, name := range []string{"X-Ea-Rule-Last-Modified", "X-Ef-Rule-Last-Modified"} {
		t, err := time.Parse(ruleTimeLayout, strings.TrimSpace(h.Get(name)))
		if err == nil && t.After(e.RuleModified) {
			e.RuleModified = t
		}
	}
	return e
}
