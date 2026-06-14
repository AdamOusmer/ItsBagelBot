package web

import (
	"sort"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"itsbagelbot/admin/ui"
)

// A "lane" is one JetStream durable consumer (created by the fleet's
// subscribers, see pkg/bus). Every number below is read live from the broker
// over the admin's existing NATS connection — nothing here is simulated.
//
// The data path is StreamsInfo -> per-stream ConsumersInfo -> Lane, sampled on
// a short TTL so repeated htmx polls within a tick do not hammer the JetStream
// API. Throughput is the only derived value, and it is derived from two real
// samples of the consumer's monotonic Delivered.Consumer counter.

// The rendered Lane type lives in package ui (ui.Lane) so the templ components
// can share it without an import cycle.

// laneSample is one prior observation of a consumer's delivery counter.
type laneSample struct {
	delivered uint64
	at        time.Time
}

// laneSampler holds the previous delivery sample per (stream, consumer) so it
// can compute a real per-second throughput across two ticks. It is shared
// across requests, so it carries its own cache + expiry + mutex, mirroring the
// snapshot()/userStats() TTL-cache pattern on Server.
type laneSampler struct {
	prev  map[string]laneSample
	value []ui.Lane
	err   string
	until time.Time
}

func newLaneSampler() *laneSampler {
	return &laneSampler{prev: make(map[string]laneSample)}
}

func laneKey(stream, consumer string) string { return stream + "\x00" + consumer }

// collect enumerates every stream and its consumers over js, diffs each
// consumer's Delivered.Consumer counter against the prior sample to get a real
// throughput, and computes a bounded load/backlog fill. It returns the lanes
// plus a non-empty error string when the JetStream API is unreachable or the
// admin account lacks $JS.API permission. now is passed in for testability.
func (l *laneSampler) collect(js nats.JetStreamContext, now time.Time) ([]ui.Lane, string) {
	var lanes []ui.Lane
	seen := make(map[string]struct{})
	streamsSeen := 0

	// First pass: gather raw consumer info and compute throughput. StreamsInfo
	// and ConsumersInfo return channels; ranging drains them.
	//
	// We deliberately do NOT probe with AccountInfo first: the hub runs
	// JetStream with a domain (domain: hub in nats-server.conf), so a bare
	// $JS.API.INFO request has no responder and AccountInfo times out, even
	// though the stream/consumer list calls below ARE served on the bare API
	// (pkg/bus provision.go provisions streams over the same bare context).
	// Reachability is inferred from whether any stream comes back instead.
	for stream := range js.StreamsInfo() {
		if stream == nil {
			continue
		}
		streamsSeen++
		streamName := stream.Config.Name
		for ci := range js.ConsumersInfo(streamName) {
			if ci == nil {
				continue
			}
			key := laneKey(streamName, ci.Name)
			seen[key] = struct{}{}

			filter := ci.Config.FilterSubject
			ephemeral := ci.Config.Durable == ""
			lane := ui.Lane{
				Stream:      streamName,
				Consumer:    ci.Name,
				Filter:      filter,
				Ephemeral:   ephemeral,
				Orphan:      !ci.PushBound,
				Group:       laneGroup(ci.Name, filter, ephemeral),
				Category:    laneCategory(streamName, ephemeral),
				Pending:     ci.NumPending,
				AckPending:  ci.NumAckPending,
				MaxAckPend:  ci.Config.MaxAckPending,
				Redelivered: ci.NumRedelivered,
				Delivered:   ci.Delivered.Consumer,
			}

			// Throughput from two real samples. No prior sample => no rate yet.
			if prev, ok := l.prev[key]; ok {
				secs := now.Sub(prev.at).Seconds()
				if secs > 0 {
					delta := float64(ci.Delivered.Consumer) - float64(prev.delivered)
					if delta < 0 {
						// Consumer was reset/recreated and the monotonic counter
						// went backwards; a negative rate is meaningless.
						delta = 0
					}
					lane.Rate = delta / secs
					lane.HasRate = true
				}
			}
			l.prev[key] = laneSample{delivered: ci.Delivered.Consumer, at: now}

			lanes = append(lanes, lane)
		}
	}

	// StreamsInfo's channel closes empty both when the JetStream API is
	// unreachable (no responder / no permission) and when the broker genuinely
	// has no streams. The fleet always provisions BAGEL_DATA, TWITCH_INGRESS and
	// TWITCH_OUTGRESS (see pkg/bus DataStreams), so zero streams here means the
	// admin account could not read the JetStream API, not an empty broker.
	if streamsSeen == 0 {
		return nil, "JetStream API unreachable: no streams returned (broker unreachable or account lacks $JS.API access)"
	}

	// Drop stale samples for consumers that no longer exist so the map does not
	// grow without bound across recreations.
	for key := range l.prev {
		if _, ok := seen[key]; !ok {
			delete(l.prev, key)
		}
	}

	// Second pass: compute the bounded fill. Preferred meaning is in-flight
	// saturation (a true load gauge) when the consumer caps in-flight; the
	// fallback for unlimited consumers is backlog pressure relative to the
	// busiest visible lane.
	var maxPending uint64
	for _, lane := range lanes {
		if lane.MaxAckPend <= 0 && lane.Pending > maxPending {
			maxPending = lane.Pending
		}
	}
	for i := range lanes {
		lane := &lanes[i]
		if lane.MaxAckPend > 0 {
			lane.FillIsLoad = true
			lane.FillPct = float64(lane.AckPending) / float64(lane.MaxAckPend) * 100
		} else if maxPending > 0 {
			lane.FillIsLoad = false
			lane.FillPct = float64(lane.Pending) / float64(maxPending) * 100
		} else {
			lane.FillPct = 0
		}
		lane.FillPct = clampPct(lane.FillPct)
	}

	sortLanes(lanes)
	return lanes, ""
}

// clampPct bounds a percentage into [0,100]; MaxAckPending can be momentarily
// exceeded during redelivery accounting, and we never render an overfull bar.
func clampPct(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

// laneGroup derives the service/queue group from a durable consumer name. The
// fleet names durables <group>_<subjectToken(filter)> (see pkg/bus durableName),
// so stripping the tokenised filter suffix recovers the group. Ephemeral
// consumers have random names and no meaningful group.
func laneGroup(name, filter string, ephemeral bool) string {
	if ephemeral {
		return ""
	}
	if filter != "" {
		if g := strings.TrimSuffix(name, "_"+subjectToken(filter)); g != name {
			return g
		}
	}
	return name
}

// subjectToken mirrors pkg/bus.subjectToken: dots and wildcards become
// underscores so the value is usable inside a JetStream consumer name.
func subjectToken(subject string) string {
	return strings.NewReplacer(".", "_", "*", "_", ">", "_").Replace(subject)
}

// laneCategory buckets a lane for sectioning in the UI. The outgress stream
// carries the chat egress "system" lanes (premium/standard/system); other
// durable consumers fold data-plane events (projections); ephemeral consumers
// are the per-pod cache-invalidation broadcast subscribers.
func laneCategory(stream string, ephemeral bool) string {
	switch {
	case ephemeral:
		return "ephemeral"
	case stream == "TWITCH_OUTGRESS":
		return "system"
	default:
		return "projection"
	}
}

// categoryRank orders the sections: system lanes first (the ones operators
// watch), then projections, then the ephemeral noise.
func categoryRank(category string) int {
	switch category {
	case "system":
		return 0
	case "projection":
		return 1
	default:
		return 2
	}
}

// sortLanes orders by section, then stream, then a stable name so ephemerals
// (random names) at least stay grouped by subject via Group/Filter fallbacks.
func sortLanes(lanes []ui.Lane) {
	sort.Slice(lanes, func(i, j int) bool {
		a, b := lanes[i], lanes[j]
		if ra, rb := categoryRank(a.Category), categoryRank(b.Category); ra != rb {
			return ra < rb
		}
		if a.Stream != b.Stream {
			return a.Stream < b.Stream
		}
		if a.Filter != b.Filter {
			return a.Filter < b.Filter
		}
		return a.Consumer < b.Consumer
	})
}
