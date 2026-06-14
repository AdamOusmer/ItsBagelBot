package web

import (
	"sort"
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
	// Probe the JetStream API first so a permissions/connectivity failure
	// surfaces as a clean error state rather than an empty list. AccountInfo
	// requires $JS.API.INFO, which any account with JetStream access has.
	if _, err := js.AccountInfo(nats.MaxWait(2 * time.Second)); err != nil {
		return nil, err.Error()
	}

	var lanes []ui.Lane
	seen := make(map[string]struct{})

	// First pass: gather raw consumer info and compute throughput. StreamsInfo
	// and ConsumersInfo return channels; ranging drains them.
	for stream := range js.StreamsInfo() {
		if stream == nil {
			continue
		}
		streamName := stream.Config.Name
		for ci := range js.ConsumersInfo(streamName) {
			if ci == nil {
				continue
			}
			key := laneKey(streamName, ci.Name)
			seen[key] = struct{}{}

			lane := ui.Lane{
				Stream:      streamName,
				Consumer:    ci.Name,
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

// sortLanes gives a stable, readable order: grouped by stream, then consumer.
func sortLanes(lanes []ui.Lane) {
	sort.Slice(lanes, func(i, j int) bool {
		if lanes[i].Stream != lanes[j].Stream {
			return lanes[i].Stream < lanes[j].Stream
		}
		return lanes[i].Consumer < lanes[j].Consumer
	})
}
