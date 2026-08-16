// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
)

const (
	messagingSystemAttribute      = "messaging.system"
	messagingOperationAttribute   = "messaging.operation"
	messagingDestinationAttribute = "messaging.destination"
	queueMillisAttribute          = "messaging.queue_ms"
	sampledAttribute              = "sampled"
	resultAttribute               = "result"
	rpcDestinationPrefix          = "bagel.rpc."
	ingressDestinationPrefix      = "twitch.ingress.event."
	outgressDestinationPrefix     = "twitch.outgress."
	cacheDestinationPrefix        = "bagel.cache.invalidate"
)

// resultOK and resultDeferred are the two processResult values the consume path
// branches on. They are named because the classification is computed exactly
// once per message (see consumeLane.process) and then compared, rather than
// re-deriving the outcome from the error three times per delivery.
const (
	resultOK       = "ok"
	resultDeferred = "deferred"
)

var rpcDestinations = map[string]struct{}{
	"admin": {}, "broadcaster": {}, "commands": {}, "dashboard": {},
	"delegation": {}, "gossip": {}, "health": {}, "ingress": {},
	"internal": {}, "loyalty": {}, "modules": {}, "notifications": {},
	"outgress": {}, "projector": {}, "transactions": {}, "users": {},
}

type destinationFamily struct {
	fallback string
	allowed  map[string]struct{}
}

type messagingSpan struct {
	name        string
	operation   string
	destination string
}

type messagingAttributes struct {
	operation   string
	destination string
}

func (span messagingSpan) attributes() messagingAttributes {
	return messagingAttributes{operation: span.operation, destination: span.destination}
}

// attributeSink is the write side both New Relic carriers share. Segment and
// Transaction spell the same method, so the messaging triple is written in one
// place; two copies of it could report a segment and its transaction under
// different destinations.
type attributeSink interface {
	AddAttribute(key string, value any)
}

// addMessagingAttributes writes the bounded messaging triple. The destination is
// normalized here rather than at the call sites, so no carrier can ever be given
// a raw wire subject.
func addMessagingAttributes(sink attributeSink, attributes messagingAttributes) {
	sink.AddAttribute(messagingSystemAttribute, "nats")
	sink.AddAttribute(messagingOperationAttribute, attributes.operation)
	sink.AddAttribute(messagingDestinationAttribute, normalizedDestination(attributes.destination))
}

var ingressDestinations = destinationFamily{
	fallback: ingressDestinationPrefix + "other",
	allowed: map[string]struct{}{
		ingressDestinationPrefix + "premium":  {},
		ingressDestinationPrefix + "standard": {},
		ingressDestinationPrefix + "stream":   {},
	},
}

var outgressDestinations = destinationFamily{
	fallback: outgressDestinationPrefix + "other",
	allowed: map[string]struct{}{
		outgressDestinationPrefix + "premium":  {},
		outgressDestinationPrefix + "standard": {},
		outgressDestinationPrefix + "system":   {},
	},
}

// startMessagingSegment creates a fixed-name span and puts the configured
// destination in an attribute. Keeping subjects out of span names prevents an
// accidentally ID-expanded subject from creating unbounded metric names.
func startMessagingSegment(ctx context.Context, span messagingSpan) *newrelic.Segment {
	txn := newrelic.FromContext(ctx)
	if txn == nil {
		return nil
	}
	segment := txn.StartSegment(span.name)
	addMessagingAttributes(segment, span.attributes())
	return segment
}

func endMessagingSegment(segment *newrelic.Segment, err error) {
	if segment == nil {
		return
	}
	segment.AddAttribute(resultAttribute, messagingResult(err))
	segment.End()
}

func messagingResult(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, nats.ErrTimeout):
		return "timeout"
	default:
		return "error"
	}
}

func insertTraceHeaders(ctx context.Context, msg *nats.Msg) {
	txn := newrelic.FromContext(ctx)
	if txn == nil {
		return
	}
	headers := http.Header{}
	txn.InsertDistributedTraceHeaders(headers)
	for key := range headers {
		msg.Header.Set(key, headers.Get(key))
	}
}

// traceHeaderBuffer allocates the agent-shaped map a consume path fills, or
// reports false when there is no transaction to join or nothing to join it from.
// The guard and the sizing live here so the two carriers below cannot disagree
// about which deliveries are traceable.
func traceHeaderBuffer(txn *newrelic.Transaction, size int) (http.Header, bool) {
	if txn == nil || size == 0 {
		return nil, false
	}
	return make(http.Header, size), true
}

// acceptQueueTrace joins the publisher's trace. The transport kind is named in
// one place: every delivery this package accepts is a queue hop, and a second
// spelling would split one distributed trace in two.
func acceptQueueTrace(txn *newrelic.Transaction, headers http.Header) {
	txn.AcceptDistributedTraceHeaders(newrelic.TransportQueue, headers)
}

func acceptTraceHeaders(txn *newrelic.Transaction, headers nats.Header) {
	httpHeaders, ok := traceHeaderBuffer(txn, len(headers))
	if !ok {
		return
	}
	for key, values := range headers {
		for _, value := range values {
			httpHeaders.Add(key, value)
		}
	}
	acceptQueueTrace(txn, httpHeaders)
}

// acceptMetadataTraceHeaders joins the publisher's trace from an already
// decoded Metadata. It is the consume path's variant of acceptTraceHeaders and
// exists to materialize one map per delivery instead of two: routing Metadata
// through a temporary nats.Header only to copy it into an http.Header allocated
// a whole intermediate header set on every message of every lane. Metadata,
// nats.Header and http.Header are all string-keyed maps over the same wire
// headers, so the intermediate carried no information.
//
// Set, not a verbatim map write: the canonicalization is load-bearing. Both
// nats.Header and Metadata are case-sensitive over the casing the publisher put
// on the wire (nats.go preserves the original case when it decodes a header
// block, and fleetMetadata copies those keys through), so a publisher that sent
// a lowercase "traceparent" leaves a lowercase key here. The New Relic agent
// looks its trace headers up with http.Header.Get, which canonicalizes the
// lookup key and therefore only ever finds a canonical map key. http.Header.Set
// applies exactly the canonicalization http.Header.Add applied in the two-map
// version, so this stays a pure allocation change.
func acceptMetadataTraceHeaders(txn *newrelic.Transaction, metadata Metadata) {
	headers, ok := traceHeaderBuffer(txn, len(metadata))
	if !ok {
		return
	}
	for key, value := range metadata {
		headers.Set(key, value)
	}
	acceptQueueTrace(txn, headers)
}

func addMessagingTransactionAttributes(txn *newrelic.Transaction, attributes messagingAttributes) {
	if txn == nil {
		return
	}
	addMessagingAttributes(txn, attributes)
}

// normalizedDestination intentionally reports a configured family rather than
// the raw wire subject. Raw subjects are still available in correlated logs;
// keeping them out of APM facets prevents IDs or tenant tokens from creating
// unbounded cardinality.
func normalizedDestination(subject string) string {
	switch {
	case strings.HasPrefix(subject, rpcDestinationPrefix):
		return normalizedRPCDestination(subject)
	case strings.HasPrefix(subject, ingressDestinationPrefix):
		return ingressDestinations.normalize(subject)
	case strings.HasPrefix(subject, outgressDestinationPrefix):
		return outgressDestinations.normalize(subject)
	case strings.HasPrefix(subject, cacheDestinationPrefix):
		return cacheDestinationPrefix
	default:
		return "other"
	}
}

func normalizedRPCDestination(subject string) string {
	remainder := strings.TrimPrefix(subject, rpcDestinationPrefix)
	service, _, _ := strings.Cut(remainder, ".")
	if _, ok := rpcDestinations[service]; ok {
		return rpcDestinationPrefix + service
	}
	return rpcDestinationPrefix + "other"
}

func (family destinationFamily) normalize(subject string) string {
	if _, ok := family.allowed[subject]; ok {
		return subject
	}
	return family.fallback
}

// --- consume lane sampling and side-channel counters ---------------------
//
// A New Relic transaction per consumed message is the single most expensive
// item in the consumer's per-message budget at lane rate — heavier than decode,
// because it allocates, builds attribute maps, and contends on the agent's
// harvest buffer. The lanes target 100k msg/s per pod with roughly 10µs of
// total budget per message, which one transaction can spend on its own.
//
// So the transaction moves off the hot path without the observability moving
// with it: one message in every consumeSampleRate gets the full treatment
// (trace join, messaging attributes, message.process segment, and therefore the
// instrumented database's datastore spans), every message is counted in atomic
// lane counters, and one goroutine per process turns those counters into a
// custom event every laneTelemetryInterval. Errors are exempt from sampling
// entirely (see consumeLane.processUnsampled).

const (
	// consumeNRSampleRate is how many consumed messages share one full New Relic
	// transaction, fixed rather than configurable: a full go-agent transaction
	// costs ~7µs and 63 allocations — the entire per-message budget of a 100k
	// msg/s lane — so paying it per message is the option that would need
	// justifying. One fully traced message in a hundred keeps distributed traces
	// and datastore spans flowing on every lane (a low-traffic lane still traces
	// its first delivery immediately) while the other ninety-nine run the
	// zero-allocation path and are counted by the side channel below. Errors
	// bypass sampling entirely, so nothing that fails is ever invisible.
	consumeNRSampleRate = 100

	// laneTelemetryInterval paces the side channel. It is long enough that the
	// flush cost is irrelevant next to the traffic it summarises, and short
	// enough that a lane going silent is visible within one dashboard refresh.
	laneTelemetryInterval = 5 * time.Second

	// laneTelemetryEventType is the custom event the counters are reported as.
	// An event, not a custom metric: the lane has to be a facet, and a metric
	// could only carry the lane by encoding it in the metric NAME, which is the
	// unbounded-cardinality trap normalizedDestination exists to avoid. At one
	// event per lane per interval the map allocation is free.
	laneTelemetryEventType = "BagelBusConsume"
)

// laneStats is one lane's telemetry state: the sampling cursor plus the counters
// that account for the messages sampling skipped. It is shared by pointer across
// every consumer unit reading that lane (consumeLane is copied by value into
// each dispatch), so the sample rate is honoured per lane per process rather
// than per unit, and unit churn cannot fragment the counts.
//
// processed is deliberately NOT a field: it is ok+deferred+failed, so deriving
// it at flush saves one atomic read-modify-write on every single delivery. The
// three are read one after another, so a message landing mid-drain can be
// counted in the next window; over a 5s aggregate that is noise.
type laneStats struct {
	// destination is the normalized (bounded) family, not the wire subject.
	destination string

	// sampleRate is fixed for the lane's life. It is a field rather than a
	// package read so a future per-lane override only has to set it.
	sampleRate uint64
	// sampleCursor is the deterministic 1-in-N cursor. Atomic add and one
	// integer division, never a rand source (global lock) and never a clock.
	sampleCursor atomic.Uint64

	ok       atomic.Uint64
	deferred atomic.Uint64
	failed   atomic.Uint64

	// queueMicros/queueMaxMicros accumulate the delivery wait that the sampled
	// transaction reports as messaging.queue_ms, so the unsampled majority still
	// shows up as an average and a high-water mark.
	queueMicros    atomic.Uint64
	queueMaxMicros atomic.Uint64
}

// sample reports whether this delivery gets the full New Relic treatment. At
// rate 1 it is a single predictable branch with no atomic at all, so the default
// configuration is strictly cheaper than the unconditional transaction it
// replaces. Above 1 it costs one atomic add and one division.
//
// The first delivery of a lane is always sampled (the cursor is compared after
// subtracting its own increment), so a low-traffic lane produces a trace
// immediately instead of after N messages.
func (s *laneStats) sample() bool {
	if s == nil || s.sampleRate <= 1 {
		return true
	}
	return (s.sampleCursor.Add(1)-1)%s.sampleRate == 0
}

// record accounts for one finished delivery. Every message lands here, sampled
// or not, so the counters are the lane's whole truth and no dashboard has to add
// the sampled and unsampled populations together.
func (s *laneStats) record(result string, wait time.Duration) {
	if s == nil {
		return
	}
	switch result {
	case resultOK:
		s.ok.Add(1)
	case resultDeferred:
		s.deferred.Add(1)
	default:
		s.failed.Add(1)
	}
	micros := uint64(wait.Microseconds())
	s.queueMicros.Add(micros)
	storeMax(&s.queueMaxMicros, micros)
}

// storeMax raises target to value when value is larger. The load-only fast path
// matters: on the hot lane almost every wait is below the window's high-water
// mark, so the common case is a plain atomic load and a branch.
func storeMax(target *atomic.Uint64, value uint64) {
	for {
		current := target.Load()
		if value <= current {
			return
		}
		if target.CompareAndSwap(current, value) {
			return
		}
	}
}

// drain empties the window and renders it as custom event parameters, reporting
// false when nothing was processed. Silent lanes emit nothing at all: a fleet
// carries many idle lanes and a zero event per lane per interval would be pure
// harvest noise.
//
// The counters are reset by the read, so a window is reported at most once and a
// dropped event loses that window rather than double-counting the next one.
func (s *laneStats) drain() (map[string]any, bool) {
	ok := s.ok.Swap(0)
	deferred := s.deferred.Swap(0)
	failed := s.failed.Swap(0)

	processed := ok + deferred + failed
	if processed == 0 {
		return nil, false
	}

	micros := s.queueMicros.Swap(0)
	peak := s.queueMaxMicros.Swap(0)

	return map[string]any{
		messagingSystemAttribute:      "nats",
		messagingOperationAttribute:   "process",
		messagingDestinationAttribute: s.destination,
		"sample_rate":                 float64(s.sampleRate),
		"processed":                   float64(processed),
		resultOK:                      float64(ok),
		resultDeferred:                float64(deferred),
		"failed":                      float64(failed),
		"queue_ms.avg":                float64(micros) / float64(processed) / 1000,
		"queue_ms.max":                float64(peak) / 1000,
	}, true
}

// laneTelemetry is the process-wide registry of lane counters and the owner of
// the single flusher goroutine.
//
// Lifetime, deliberately: consumeLane has no Close, and the weighted consumer
// creates and retires consumer units for the life of the process, each building
// its own consumeLane per lane. Anything owned per unit would therefore have to
// be torn down on every retirement, and anything registered per unit would grow
// without bound. Registration is interned by normalized destination instead, so
// the registry is bounded by the destination families in telemetry.go no matter
// how many units churn, and the flusher is exactly ONE goroutine for the life of
// the process, started only once there is an application to report to. It holds
// nothing but this registry, so it cannot pin retired units. The final,
// partially elapsed window is lost at shutdown; at a 5s interval that is a
// deliberate trade against wiring a stop signal through a constructor whose
// signature the weighted consumer depends on.
type laneTelemetry struct {
	mu    sync.Mutex
	lanes map[string]*laneStats
	start sync.Once
}

var consumeTelemetry = newLaneTelemetry()

func newLaneTelemetry() *laneTelemetry {
	return &laneTelemetry{lanes: make(map[string]*laneStats)}
}

// register returns the shared counters for subject's destination family,
// creating them on first use, and makes sure the flusher is running. Two
// subjects that normalize to the same family share one entry: that is the same
// bounded-cardinality bargain APM already makes for this lane, and it is what
// keeps the registry from growing with ID-bearing subjects.
func (t *laneTelemetry) register(app *newrelic.Application, subject string, rate uint64) *laneStats {
	destination := normalizedDestination(subject)

	t.mu.Lock()
	stats, ok := t.lanes[destination]
	if !ok {
		stats = &laneStats{destination: destination, sampleRate: rate}
		t.lanes[destination] = stats
	}
	t.mu.Unlock()

	t.startFlusher(app)
	return stats
}

// startFlusher launches the side channel at most once per process. A nil
// application (no license key, and every unit test) starts nothing: the counters
// still tick harmlessly, exactly like every other New Relic call on a nil app.
func (t *laneTelemetry) startFlusher(app *newrelic.Application) {
	if app == nil {
		return
	}
	t.start.Do(func() { go t.flushLoop(app) })
}

func (t *laneTelemetry) flushLoop(app *newrelic.Application) {
	ticker := time.NewTicker(laneTelemetryInterval)
	defer ticker.Stop()
	for range ticker.C {
		t.flush(app)
	}
}

// flush snapshots the registered lanes under the lock and records them outside
// it, so a slow agent call can never block a lane's registration.
func (t *laneTelemetry) flush(app *newrelic.Application) {
	t.mu.Lock()
	lanes := make([]*laneStats, 0, len(t.lanes))
	for _, stats := range t.lanes {
		lanes = append(lanes, stats)
	}
	t.mu.Unlock()

	for _, stats := range lanes {
		if params, ok := stats.drain(); ok {
			app.RecordCustomEvent(laneTelemetryEventType, params)
		}
	}
}
