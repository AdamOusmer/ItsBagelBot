// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

type attributeSink interface {
	AddAttribute(key string, value any)
}

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

func traceHeaderBuffer(txn *newrelic.Transaction, size int) (http.Header, bool) {
	if txn == nil || size == 0 {
		return nil, false
	}
	return make(http.Header, size), true
}

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

const (
	consumeNRSampleRate = 100

	laneTelemetryInterval = 5 * time.Second

	laneTelemetryEventType = "BagelBusConsume"
)

type laneStats struct {
	destination string

	sampleRate   uint64
	sampleCursor atomic.Uint64

	ok       atomic.Uint64
	deferred atomic.Uint64
	failed   atomic.Uint64

	queueMicros    atomic.Uint64
	queueMaxMicros atomic.Uint64
}

func (s *laneStats) sample() bool {
	if s == nil || s.sampleRate <= 1 {
		return true
	}
	return (s.sampleCursor.Add(1)-1)%s.sampleRate == 0
}

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

type laneTelemetry struct {
	mu    sync.Mutex
	lanes map[string]*laneStats
	start sync.Once
}

var consumeTelemetry = newLaneTelemetry()

func newLaneTelemetry() *laneTelemetry {
	return &laneTelemetry{lanes: make(map[string]*laneStats)}
}

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
