package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// Lane telemetry RPC. A "lane" is one JetStream durable (or ephemeral)
// consumer created by the fleet's subscribers (see pkg/bus). The retired Go
// admin read this directly over its own NATS connection and rendered it; the
// SvelteKit console cannot speak the JetStream management API (its client is a
// core request/reply helper), so this service answers on its behalf.
//
// Every number is read live from the broker — nothing is simulated. The data
// path is StreamsInfo -> per-stream ConsumersInfo -> laneWire, sampled by a
// background ticker so each `.get` reply carries a real two-sample throughput
// without re-walking the JetStream API per request.
//
// Subjects live under the existing admin-user prefix (bagel.rpc.admin.user.*):
// the console's `admin` NATS user already publishes that wildcard and the
// `users` service user already subscribes it and holds $JS.> permission, so the
// lane view needs no broker-auth change to come online.

// laneWire is the exact wire contract the console's LaneView type expects. Field
// names and JSON shape must not drift from console/admin/src/lib/server/lanes.ts.
type laneWire struct {
	Stream      string `json:"stream"`
	Consumer    string `json:"consumer"`
	Display     string `json:"display"`
	Subject     string `json:"subject"`
	Category    string `json:"category"` // "system" | "projection" | "ephemeral"
	Ephemeral   bool   `json:"ephemeral"`
	Orphan      bool   `json:"orphan"`
	Pending     uint64 `json:"pending"`
	InFlight    string `json:"inFlight"`
	Rate        string `json:"rate"`
	Redelivered int    `json:"redelivered"`
}

type lanesReply struct {
	Lanes []laneWire `json:"lanes"`
	Error string     `json:"error,omitempty"`
}

type laneMutationReply struct {
	OK     bool   `json:"ok"`
	Notice string `json:"notice,omitempty"`
	Error  string `json:"error,omitempty"`
}

type laneMutationRequest struct {
	Stream   string `json:"stream"`
	Consumer string `json:"consumer"`
	Alias    string `json:"alias"`
}

// SubscribeLanes wires the lane telemetry + mutation RPC on the admin prefix and
// starts the background sampler. The sampler runs until ctx is cancelled.
func SubscribeLanes(ctx context.Context, nc *nats.Conn, prefix, queueGroup string, log *zap.Logger) error {
	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("lanes: jetstream context: %w", err)
	}

	s := &laneSampler{js: js, store: newLaneStore(js), prev: map[string]laneSample{}, log: log}
	s.sample() // prime one observation so a rate appears on the next tick
	go s.run(ctx)

	handlers := map[string]nats.MsgHandler{
		"lanes.get": func(msg *nats.Msg) {
			lanes, errMsg := s.snapshot()
			respondJSON(msg, lanesReply{Lanes: lanes, Error: errMsg})
		},
		"lanes.alias":   s.handleAlias,
		"lanes.durable": s.handleDurable,
		"lanes.delete":  s.handleDelete,
	}

	for verb, handle := range handlers {
		subject := prefix + "." + verb
		if _, err := nc.QueueSubscribe(subject, queueGroup, handle); err != nil {
			return fmt.Errorf("lanes: subscribe %s: %w", subject, err)
		}
	}
	return nil
}

func respondJSON(msg *nats.Msg, v any) {
	body, _ := json.Marshal(v)
	_ = msg.Respond(body)
}

// laneSample is one prior observation of a consumer's delivery counter.
type laneSample struct {
	delivered uint64
	at        time.Time
}

// laneSampler holds the previous delivery sample per (stream, consumer) so it
// can compute a real per-second throughput across two ticks, plus the latest
// rendered snapshot. It is shared across requests behind a mutex.
type laneSampler struct {
	js    nats.JetStreamContext
	store *laneStore
	log   *zap.Logger

	mu      sync.Mutex
	prev    map[string]laneSample
	value   []laneWire
	lastErr string
}

func laneKey(stream, consumer string) string { return stream + "\x00" + consumer }

// run samples on a 2s cadence (the same window the retired admin polled at) so
// any `.get` reply has a fresh two-sample rate, until ctx is cancelled.
func (s *laneSampler) run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sample()
		}
	}
}

// snapshot returns the last rendered lanes and error string.
func (s *laneSampler) snapshot() ([]laneWire, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, s.lastErr
}

// sample walks the JetStream API once, recomputes throughput against the prior
// observation, merges the operator's display aliases, and stores the result.
func (s *laneSampler) sample() {
	lanes, errMsg := s.collect(time.Now())
	s.mu.Lock()
	s.value = lanes
	s.lastErr = errMsg
	s.mu.Unlock()
}

// collect enumerates every stream and its consumers, diffs each consumer's
// Delivered.Consumer counter against the prior sample for a real throughput, and
// renders the wire rows. A non-empty error string means the JetStream API is
// unreachable or this account lacks $JS.API access.
//
// It deliberately does NOT probe AccountInfo first: the hub runs JetStream with
// a domain (domain: hub in nats-server.conf), so a bare $JS.API.INFO request has
// no responder and AccountInfo times out, even though the stream/consumer list
// calls below ARE served on the bare API. Reachability is inferred from whether
// any stream comes back instead.
func (s *laneSampler) collect(now time.Time) ([]laneWire, string) {
	aliases := s.store.aliases()

	type raw struct {
		stream      string
		consumer    string
		filter      string
		ephemeral   bool
		orphan      bool
		category    string
		group       string
		pending     uint64
		ackPending  int
		maxAckPend  int
		redelivered int
		delivered   uint64
		rate        float64
		hasRate     bool
	}

	var rows []raw
	seen := map[string]struct{}{}
	streamsSeen := 0

	for stream := range s.js.StreamsInfo() {
		if stream == nil {
			continue
		}
		streamsSeen++
		streamName := stream.Config.Name
		for ci := range s.js.ConsumersInfo(streamName) {
			if ci == nil {
				continue
			}
			key := laneKey(streamName, ci.Name)
			seen[key] = struct{}{}

			filter := ci.Config.FilterSubject
			ephemeral := ci.Config.Durable == ""
			r := raw{
				stream:      streamName,
				consumer:    ci.Name,
				filter:      filter,
				ephemeral:   ephemeral,
				orphan:      !ci.PushBound,
				category:    laneCategory(streamName, ephemeral),
				group:       laneGroup(ci.Name, filter, ephemeral),
				pending:     ci.NumPending,
				ackPending:  ci.NumAckPending,
				maxAckPend:  ci.Config.MaxAckPending,
				redelivered: ci.NumRedelivered,
				delivered:   ci.Delivered.Consumer,
			}

			// Throughput from two real samples. No prior sample => no rate yet.
			if prev, ok := s.prev[key]; ok {
				if secs := now.Sub(prev.at).Seconds(); secs > 0 {
					delta := float64(ci.Delivered.Consumer) - float64(prev.delivered)
					if delta < 0 {
						// Consumer was reset/recreated and the monotonic counter
						// went backwards; a negative rate is meaningless.
						delta = 0
					}
					r.rate = delta / secs
					r.hasRate = true
				}
			}
			s.prev[key] = laneSample{delivered: ci.Delivered.Consumer, at: now}

			rows = append(rows, r)
		}
	}

	// StreamsInfo's channel closes empty both when the JetStream API is
	// unreachable (no responder / no permission) and when the broker genuinely
	// has no streams. The fleet always provisions BAGEL_DATA, TWITCH_INGRESS and
	// TWITCH_OUTGRESS (see pkg/bus DataStreams), so zero streams here means this
	// account could not read the JetStream API, not an empty broker.
	if streamsSeen == 0 {
		return nil, "JetStream API unreachable: no streams returned (broker unreachable or account lacks $JS.API access)"
	}

	// Drop stale samples for consumers that no longer exist so the map does not
	// grow without bound across recreations.
	for key := range s.prev {
		if _, ok := seen[key]; !ok {
			delete(s.prev, key)
		}
	}

	sortRows := func(i, j int) bool {
		a, b := rows[i], rows[j]
		if ra, rb := categoryRank(a.category), categoryRank(b.category); ra != rb {
			return ra < rb
		}
		if a.stream != b.stream {
			return a.stream < b.stream
		}
		if a.filter != b.filter {
			return a.filter < b.filter
		}
		return a.consumer < b.consumer
	}
	sort.SliceStable(rows, sortRows)

	lanes := make([]laneWire, 0, len(rows))
	for _, r := range rows {
		alias := aliases[laneAliasKey(r.stream, r.consumer)]
		lanes = append(lanes, laneWire{
			Stream:      r.stream,
			Consumer:    r.consumer,
			Display:     displayName(alias, r.group, r.consumer, r.ephemeral),
			Subject:     r.filter,
			Category:    r.category,
			Ephemeral:   r.ephemeral,
			Orphan:      r.orphan,
			Pending:     r.pending,
			InFlight:    inFlightText(r.ackPending, r.maxAckPend),
			Rate:        rateText(r.rate, r.hasRate),
			Redelivered: r.redelivered,
		})
	}
	return lanes, ""
}

func (s *laneSampler) handleAlias(msg *nats.Msg) {
	var req laneMutationRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondJSON(msg, laneMutationReply{Error: "bad request"})
		return
	}
	if req.Stream == "" || req.Consumer == "" {
		respondJSON(msg, laneMutationReply{Error: "rename failed: missing lane"})
		return
	}
	alias := strings.TrimSpace(req.Alias)
	if len(alias) > 48 {
		alias = alias[:48]
	}
	if err := s.store.setAlias(req.Stream, req.Consumer, alias); err != nil {
		s.log.Warn("lane alias failed", zap.String("consumer", req.Consumer), zap.Error(err))
		respondJSON(msg, laneMutationReply{Error: "rename failed: " + err.Error()})
		return
	}
	s.sample() // reflect the new label on the next read immediately
	if alias == "" {
		respondJSON(msg, laneMutationReply{OK: true, Notice: "alias cleared"})
		return
	}
	respondJSON(msg, laneMutationReply{OK: true, Notice: "renamed to " + alias})
}

// handleDurable converts an ephemeral lane into a permanent durable consumer
// with the same subject filter. Nothing drains it, so it retains new messages
// until the stream's own retention reclaims them. Refused on already-durable
// lanes.
func (s *laneSampler) handleDurable(msg *nats.Msg) {
	var req laneMutationRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondJSON(msg, laneMutationReply{Error: "bad request"})
		return
	}
	info, err := s.js.ConsumerInfo(req.Stream, req.Consumer)
	if err != nil {
		respondJSON(msg, laneMutationReply{Error: "make-permanent failed: " + err.Error()})
		return
	}
	if info.Config.Durable != "" {
		respondJSON(msg, laneMutationReply{Error: "lane is already durable"})
		return
	}
	name := "adminperm_" + subjectToken(info.Config.FilterSubject)
	if _, err := s.js.AddConsumer(req.Stream, &nats.ConsumerConfig{
		Durable:       name,
		Description:   "operator-pinned permanent lane (admin)",
		FilterSubject: info.Config.FilterSubject,
		AckPolicy:     nats.AckExplicitPolicy,
		DeliverPolicy: nats.DeliverNewPolicy,
	}); err != nil {
		s.log.Warn("lane make-durable failed", zap.String("consumer", req.Consumer), zap.Error(err))
		respondJSON(msg, laneMutationReply{Error: "make-permanent failed: " + err.Error()})
		return
	}
	s.log.Info("lane made permanent", zap.String("stream", req.Stream),
		zap.String("source", req.Consumer), zap.String("durable", name))
	s.sample()
	respondJSON(msg, laneMutationReply{OK: true,
		Notice: "created permanent lane " + name + " (nothing drains it; it retains until stream retention)"})
}

// handleDelete removes a consumer with no bound subscriber ("no system
// attached"). It refuses any lane whose deliver subject still has a live
// subscriber, so a briefly-restarting service is never deleted out from under
// itself.
func (s *laneSampler) handleDelete(msg *nats.Msg) {
	var req laneMutationRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondJSON(msg, laneMutationReply{Error: "bad request"})
		return
	}
	info, err := s.js.ConsumerInfo(req.Stream, req.Consumer)
	if err != nil {
		respondJSON(msg, laneMutationReply{Error: "delete failed: " + err.Error()})
		return
	}
	if info.PushBound {
		respondJSON(msg, laneMutationReply{Error: "refused: lane is bound to a running consumer, not an orphan"})
		return
	}
	if err := s.js.DeleteConsumer(req.Stream, req.Consumer); err != nil {
		s.log.Warn("lane delete failed", zap.String("consumer", req.Consumer), zap.Error(err))
		respondJSON(msg, laneMutationReply{Error: "delete failed: " + err.Error()})
		return
	}
	_ = s.store.setAlias(req.Stream, req.Consumer, "") // drop any stale alias
	s.log.Info("lane deleted", zap.String("stream", req.Stream), zap.String("consumer", req.Consumer))
	s.sample()
	respondJSON(msg, laneMutationReply{OK: true, Notice: "deleted orphan lane " + req.Consumer})
}

// displayName is the human label: the operator alias wins, then "ephemeral" for
// the per-pod broadcast subscribers whose real names are random gibberish, then
// the service/group for a durable consumer, then the raw name.
func displayName(alias, group, consumer string, ephemeral bool) string {
	if alias != "" {
		return alias
	}
	if ephemeral {
		return "ephemeral"
	}
	if group != "" {
		return group
	}
	return consumer
}

// inFlightText renders the in-flight (delivered, unacked) count, with the cap
// when the consumer bounds it.
func inFlightText(ackPending, maxAckPend int) string {
	if maxAckPend > 0 {
		return fmt.Sprintf("%d / %d", ackPending, maxAckPend)
	}
	return fmt.Sprint(ackPending)
}

// rateText renders throughput. The first time a lane is seen there is no prior
// sample, so it shows a dash rather than a fabricated number.
func rateText(rate float64, hasRate bool) string {
	if !hasRate {
		return "—"
	}
	switch {
	case rate == 0:
		return "0 msg/s"
	case rate < 10:
		return fmt.Sprintf("%.1f msg/s", rate)
	default:
		return fmt.Sprintf("%.0f msg/s", math.Round(rate))
	}
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
// carries the chat egress "system" lanes; other durable consumers fold
// data-plane events (projections); ephemeral consumers are the per-pod
// cache-invalidation broadcast subscribers.
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
