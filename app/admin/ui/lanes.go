package ui

import (
	"fmt"
	"math"
)

// Lane is the rendered telemetry for a single JetStream durable consumer (one
// "lane"). It is populated live from the broker over the admin's NATS
// connection (see internal/web/lanes.go); every field is a real measured value.
// It lives in ui so both the web handlers and the templ components can share it
// without an import cycle (components imports ui; ui must not import web).
type Lane struct {
	Stream    string // owning stream, e.g. TWITCH_OUTGRESS
	Consumer  string // raw durable/ephemeral consumer name (often gibberish for ephemerals)
	Group     string // service/queue group derived from the durable name (empty for ephemeral)
	Filter    string // subject this consumer filters on, e.g. twitch.outgress.system
	Ephemeral bool   // true when the consumer has no durable name (per-pod broadcast subscriber)
	Category  string // grouping bucket: "system", "projection" or "ephemeral"
	Alias     string // operator-set display label (admin-side, from the KV store)
	Orphan    bool   // true when a push consumer has no bound subscriber ("no system attached")

	Pending     uint64 // backlog not yet delivered to this consumer
	AckPending  int    // delivered but not yet acked (in-flight)
	MaxAckPend  int    // in-flight cap (<=0 means unlimited)
	Redelivered int
	Delivered   uint64 // monotonic delivery counter

	HasRate bool    // false on the first observation (no prior sample to diff)
	Rate    float64 // messages/second, from two real samples; clamped at 0

	FillPct    float64 // bounded 0..100 load metric
	FillIsLoad bool    // true => in-flight saturation; false => relative backlog
}

// PendingText renders the live backlog count.
func (l Lane) PendingText() string { return fmt.Sprint(l.Pending) }

// InFlightText renders the in-flight (delivered, unacked) count, with the cap
// when the consumer bounds it.
func (l Lane) InFlightText() string {
	if l.MaxAckPend > 0 {
		return fmt.Sprintf("%d / %d", l.AckPending, l.MaxAckPend)
	}
	return fmt.Sprint(l.AckPending)
}

// RateText renders throughput. The first time a lane is seen there is no prior
// sample, so it shows a dash rather than a fabricated number.
func (l Lane) RateText() string {
	if !l.HasRate {
		return "—"
	}
	r := l.Rate
	switch {
	case r == 0:
		return "0 msg/s"
	case r < 10:
		return fmt.Sprintf("%.1f msg/s", r)
	default:
		return fmt.Sprintf("%.0f msg/s", math.Round(r))
	}
}

// FillWidth is the inline bar width, e.g. "37%". CSP permits inline style="".
func (l Lane) FillWidth() string {
	return fmt.Sprintf("%.0f%%", clampPctUI(l.FillPct))
}

// FillLabel describes, honestly, what the bar measures for this lane.
func (l Lane) FillLabel() string {
	if l.FillIsLoad {
		return "in-flight load"
	}
	return "relative backlog"
}

// FillTone selects a bar tone from the real fill level so a saturated lane
// reads as hot without inventing a threshold the data does not support.
func (l Lane) FillTone() string {
	switch {
	case l.FillPct >= 90:
		return "lane-bar-fill down"
	case l.FillPct >= 60:
		return "lane-bar-fill warn"
	default:
		return "lane-bar-fill ok"
	}
}

// RedeliveredText renders the redelivery (retry) count.
func (l Lane) RedeliveredText() string { return fmt.Sprint(l.Redelivered) }

// DisplayName is the human label for the lane: the service/group for a durable
// consumer, or just "ephemeral" for the per-pod broadcast subscribers whose
// real names are random gibberish. The raw name stays available as a tooltip.
func (l Lane) DisplayName() string {
	if l.Alias != "" {
		return l.Alias
	}
	if l.Ephemeral {
		return "ephemeral"
	}
	if l.Group != "" {
		return l.Group
	}
	return l.Consumer
}

// Aliased reports whether an operator alias is overriding the derived name.
func (l Lane) Aliased() bool { return l.Alias != "" }

// SubjectText is the subject the lane consumes, the part operators actually
// care about. Falls back to a dash when the broker did not report a filter.
func (l Lane) SubjectText() string {
	if l.Filter == "" {
		return "—"
	}
	return l.Filter
}

// Stuck reports a lane that is holding delivered-but-unacked messages while not
// moving them: a likely wedged consumer worth surfacing.
func (l Lane) Stuck() bool { return l.AckPending > 0 && l.HasRate && l.Rate == 0 }

// LaneSection is a titled group of lanes for the UI (system / projections /
// ephemeral). Sectioning keeps the system lanes operators watch at the top and
// quarantines the noisy per-pod ephemerals at the bottom.
type LaneSection struct {
	Title string
	Lanes []Lane
}

// LaneSections splits already-sorted lanes into titled sections, preserving the
// category order set by the sampler (system, projection, ephemeral).
func LaneSections(lanes []Lane) []LaneSection {
	titles := map[string]string{
		"system":     "System lanes (chat egress)",
		"projection": "Projections & event consumers",
		"ephemeral":  "Cache invalidation (ephemeral)",
	}
	var out []LaneSection
	for _, l := range lanes {
		title := titles[l.Category]
		if title == "" {
			title = "Other"
		}
		if n := len(out); n > 0 && out[n-1].Title == title {
			out[n-1].Lanes = append(out[n-1].Lanes, l)
			continue
		}
		out = append(out, LaneSection{Title: title, Lanes: []Lane{l}})
	}
	return out
}

func clampPctUI(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
