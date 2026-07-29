package main

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/idempotency"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/bytedance/sonic"
	jsapi "github.com/nats-io/nats.go/jetstream"
	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// Metadata keys the consumer writes onto a delivery before handing it to the
// guard.
//
// idempotency.MessageUUIDKey is deliberately NOT used here, and the reason is
// the whole test: pkg/bus stamps a fresh NUID as Bagelbot-Message-Id on every
// publish, so the two copies of an injected duplicate arrive under two different
// Message.UUIDs and a UUID-keyed guard could never recognise them as one event.
// KeyFunc exists precisely because the stable identity of an event is a domain
// question; the rig answers it with the envelope's event id, which is what
// sesame's own dedup does with its Twitch event id.
const (
	eventIDMeta    = "Stress-Event-Id"
	eventClassMeta = "Stress-Event-Class"
	appliedMeta    = "Stress-Applied"
)

// Consumer roles under test. They are two different fleet shapes, not two
// settings of one: flow gives every pod the WHOLE lane (fan-out under the
// autoscaler, correctness resting on the guard), pull gives the fleet one shared
// durable the server distributes from (scale-out). Everything downstream of the
// binding — guard, ledger, sequence accounting, lag polling — is identical, so
// the two are directly comparable.
const (
	consumeModeFlow = "flow"
	consumeModePull = "pull"
)

type consumerConfig struct {
	URL          string
	Subject      string
	Group        string
	ConsumerID   string
	Mode         string
	Routines     int
	GuardEnabled bool
	GuardPrefix  string
	GuardTTL     time.Duration
	GuardLRU     int
	LedgerWindow time.Duration
	SampleCap    int
	TickEvery    time.Duration
	LagEvery     time.Duration
	ValkeyAddr   string
	ValkeyPass   string
}

type consumeState struct {
	cfg          consumerConfig
	log          *zap.Logger
	seq          *seqLedger
	ledger       *guardLedger
	metrics      *guardMetrics
	guardLatency *sampler
	guarded      idempotency.Handler
	failOpen     func() int64

	delivered    atomic.Int64
	guardSampled atomic.Int64
	decodeErrors atomic.Int64
	lag          lagRecorder
	startedAt    time.Time
}

func runConsumer(ctx context.Context, cfg consumerConfig, log *zap.Logger) error {
	state, closeStore, err := newConsumeState(cfg, log)
	if err != nil {
		return err
	}
	defer closeStore()

	// The exported fixed-stream binding for the mode under test. Everything past
	// it is the code the hot ingress lanes run in that mode, which is why a
	// number measured here transfers.
	sub, err := bindLane(cfg, log)
	if err != nil {
		return err
	}
	defer sub.Close()

	weighted, err := bus.ConsumeWeighted(ctx, nil,
		[]bus.WeightedLane{{Sub: sub, Subject: cfg.Subject, Handle: state.handle}},
		// Pinned, not autoscaled: a routine count that moves during the run makes
		// every latency comparison between steps a comparison of two pools. And
		// MaxConsumers stays 1 because every unit in this pod shares the one lane
		// binding in either mode — extra units would add pool slots, not consumers.
		bus.ScalePolicy{
			MinRoutines: cfg.Routines, MaxRoutines: cfg.Routines,
			MinConsumers: 1, MaxConsumers: 1,
		}, log)
	if err != nil {
		return err
	}

	go state.tick(ctx)
	go state.pollLag(ctx)
	<-ctx.Done()

	drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := weighted.Drain(drainCtx); err != nil {
		log.Warn("consumer drain timed out", zap.Error(err))
	}
	emit(state.summary())
	return nil
}

// bindLane opens the lane through the exported pkg/bus constructor for the mode
// under test. Both are the fixed-stream escape hatch the rig needs — the shadow
// stream is in no catalog — and both bind the same subject with the same group,
// so the only difference between an A run and a B run is the consumer shape.
func bindLane(cfg consumerConfig, log *zap.Logger) (bus.Subscriber, error) {
	if cfg.Mode == consumeModePull {
		return bus.NewPullLaneSubscriber(bus.PullLaneConfig{
			URL: cfg.URL, Stream: stressStreamName, Subject: cfg.Subject, Group: cfg.Group, Log: log,
		})
	}
	return bus.NewFlowLaneSubscriber(bus.FlowLaneConfig{
		URL: cfg.URL, Stream: stressStreamName, Subject: cfg.Subject, Group: cfg.Group, Log: log,
	})
}

// validConsumeMode refuses an unrecognised mode outright rather than defaulting.
// A run whose mode silently fell back to flow would be reported as a pull
// measurement, and a benchmark that lies about what it measured is worse than
// one that refuses to start.
func validConsumeMode(mode string) bool {
	return mode == consumeModeFlow || mode == consumeModePull
}

func newConsumeState(cfg consumerConfig, log *zap.Logger) (*consumeState, func(), error) {
	state := &consumeState{
		cfg: cfg, log: log,
		seq:          newSeqLedger(),
		ledger:       newGuardLedger(cfg.LedgerWindow, time.Now),
		metrics:      &guardMetrics{},
		guardLatency: newSampler(cfg.SampleCap),
		failOpen:     func() int64 { return 0 },
		startedAt:    time.Now(),
	}
	if !cfg.GuardEnabled {
		state.guarded = state.applyEffect
		return state, func() {}, nil
	}
	client, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPass)
	if err != nil {
		return nil, nil, err
	}
	state.wireGuard(client)
	return state, client.Close, nil
}

// wireGuard builds the guard exactly as sesame builds its own: a bounded per-pod
// LRU in front of a Valkey store that pins itself to the Sentinel-elected
// primary. The prefix is the one deliberate difference from sesame — a shared
// "sesame:seen:" would let bench claims collide with live claims on the same
// primary and silently suppress production events.
func (s *consumeState) wireGuard(client valkey_go.Client) {
	valkeyStore := idempotency.NewValkeyStore(client, s.cfg.GuardPrefix, s.log)
	store := idempotency.NewTiered(s.cfg.GuardLRU, valkeyStore)
	s.failOpen = valkeyStore.FailOpenCount
	s.guarded = idempotency.Guard(store, stressEventKey, s.cfg.GuardTTL, s.log, s.metrics)(s.applyEffect)
}

// stressEventKey reads the identity the handler already decoded. It never
// decodes the payload itself: the guard runs on the hot path and a second JSON
// parse per guarded message would be measuring the KeyFunc.
func stressEventKey(m *bus.Message) (string, bool) {
	id := m.Metadata.Get(eventIDMeta)
	return id, id != ""
}

// handle is the firehose handler: decode, account, and only then decide whether
// this event is guard-sampled at all. Keeping the guard behind that branch is
// what keeps the Valkey claim rate at production shape.
//
// At the default flags (1% duplicate fraction, an equally sized control set)
// roughly 2% of DISTINCT events are guard-sampled, and the per-pod LRU absorbs
// the second copy of every treatment event, so one Valkey SET NX lands per
// distinct sampled event: about 1.2k claims/s per consumer pod at 60k msg/s
// delivered, ~2.4k/s across two pods. Against the ~250k ops/s the fleet primary
// measured, that is under 1% of it — which is the point. Raising -dup-fraction
// raises this linearly.
func (s *consumeState) handle(m *bus.Message) error {
	var event envelope
	if err := sonic.ConfigFastest.Unmarshal(m.Payload, &event); err != nil {
		s.decodeErrors.Add(1)
		return nil // a malformed payload is a rig bug, not backpressure: never nack
	}
	s.delivered.Add(1)
	s.seq.observe(laneKey(event.Pub, event.Lane), event.Seq)
	if event.Class == classPlain {
		return nil
	}
	return s.guardOne(m, event)
}

func (s *consumeState) guardOne(m *bus.Message, event envelope) error {
	m.Metadata.Set(eventIDMeta, event.EventID)
	m.Metadata.Set(eventClassMeta, strconv.Itoa(int(event.Class)))
	s.guardSampled.Add(1)

	start := time.Now()
	err := s.guarded(m)
	s.guardLatency.record(time.Since(start))
	if err != nil {
		return err
	}
	// The effect stamps the delivery it ran on, so a suppression is observable
	// from out here without racing the guard's own bookkeeping.
	if m.Metadata.Get(appliedMeta) == "" {
		s.ledger.record(event.EventID, event.Class, false)
	}
	return nil
}

// applyEffect stands in for a non-idempotent handler. It is deliberately trivial:
// the run measures the guard and the transport, and a handler that did real work
// would put its own cost in every number.
func (s *consumeState) applyEffect(m *bus.Message) error {
	m.Metadata.Set(appliedMeta, "1")
	s.ledger.record(m.Metadata.Get(eventIDMeta), classFrom(m.Metadata.Get(eventClassMeta)), true)
	return nil
}

func classFrom(raw string) eventClass {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return classPlain
	}
	return eventClass(value)
}

// lagRecorder holds the 1 Hz curve. Bounded so a soak that outlives its plan
// cannot grow the summary without limit; the oldest samples go first because the
// assertion is about where the curve ended up.
//
// Locked because the poller writes it on its own goroutine while the summary
// reads it on the shutdown path, and a summary is written exactly once, at the
// moment those two are most likely to overlap.
type lagRecorder struct {
	mu      sync.Mutex
	samples []lagSample
	limit   int
}

func (r *lagRecorder) add(sample lagSample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.limit <= 0 {
		r.limit = 4096
	}
	r.samples = append(r.samples, sample)
	if len(r.samples) > r.limit {
		r.samples = r.samples[len(r.samples)-r.limit:]
	}
}

func (r *lagRecorder) snapshot() []lagSample {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]lagSample(nil), r.samples...)
}

// pollLag samples the receipt lag on its own connection. Never in the handler:
// a STREAM.INFO round trip on the hot path would add a request-reply to every
// sampled delivery and the run would be reporting the probe.
func (s *consumeState) pollLag(ctx context.Context) {
	nc, err := controlConn("nats-stress-lag")
	if err != nil {
		s.log.Warn("lag polling disabled", zap.Error(err))
		return
	}
	defer nc.Close()
	js, err := jsapi.NewWithDomain(nc, bus.JSDomain())
	if err != nil {
		s.log.Warn("lag polling disabled", zap.Error(err))
		return
	}

	ticker := time.NewTicker(s.cfg.LagEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sampleLag(ctx, js)
		}
	}
}

func (s *consumeState) sampleLag(ctx context.Context, js jsapi.JetStream) {
	ctx, cancel := context.WithTimeout(ctx, streamOpTimeout)
	defer cancel()
	stream, err := js.Stream(ctx, stressStreamName)
	if err != nil {
		return
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return
	}
	sample := lagSample{
		AtSec:         time.Since(s.startedAt).Seconds(),
		StreamLastSeq: info.State.LastSeq,
		Delivered:     s.delivered.Load(),
	}
	if consumer := s.findConsumer(ctx, stream); consumer != nil {
		sample.NumPending = consumer.NumPending
		sample.AckFloorSeq = consumer.AckFloor.Stream
	}
	s.lag.add(sample)
}

// findConsumer locates the consumer this pod is fetching from (pull) or pushed
// by (flow), by listing the stream's consumers each sample rather than
// reconstructing the durable name. Listing self-heals: the flow subscriber's
// watchdog re-provisions after a heartbeat loss, and a cached name would keep
// polling a consumer that is gone.
//
// The single-candidate path is the normal one in BOTH modes, for opposite
// reasons: pull has exactly one durable for the whole fleet by construction, and
// a single-replica flow run has exactly one because there is one pod. The suffix
// match is only the multi-pod flow case, where pkg/bus builds the durable as
// <group>_<subject-token>_<pod-token> and a Kubernetes pod name is already
// inside the character set and the 48-character bound its sanitiser applies.
func (s *consumeState) findConsumer(ctx context.Context, stream jsapi.Stream) *jsapi.ConsumerInfo {
	candidates := s.laneConsumers(ctx, stream)
	if len(candidates) == 1 {
		return candidates[0]
	}
	return ownConsumer(candidates, s.cfg.ConsumerID)
}

func (s *consumeState) laneConsumers(ctx context.Context, stream jsapi.Stream) []*jsapi.ConsumerInfo {
	want := s.laneAckPolicy()
	var candidates []*jsapi.ConsumerInfo
	// Drained to completion: abandoning the lister mid-page leaks the goroutine
	// nats.go pages it from, once per second for the length of the run.
	for info := range stream.ListConsumers(ctx).Info() {
		if info.Config.FilterSubject == s.cfg.Subject && info.Config.AckPolicy == want {
			candidates = append(candidates, info)
		}
	}
	return candidates
}

// laneAckPolicy is how the lag poller tells the two modes' consumers apart on a
// stream that may still carry a leftover from the other mode's run: the ack
// policy is the one field that cannot be the same for both.
func (s *consumeState) laneAckPolicy() jsapi.AckPolicy {
	if s.cfg.Mode == consumeModePull {
		return jsapi.AckAllPolicy
	}
	return jsapi.AckFlowControlPolicy
}

func ownConsumer(candidates []*jsapi.ConsumerInfo, identity string) *jsapi.ConsumerInfo {
	for _, info := range candidates {
		if strings.HasSuffix(info.Name, "_"+identity) {
			return info
		}
	}
	return nil
}

func (s *consumeState) tick(ctx context.Context) {
	if s.cfg.TickEvery <= 0 {
		return
	}
	ticker := time.NewTicker(s.cfg.TickEvery)
	defer ticker.Stop()
	last := s.delivered.Load()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := s.delivered.Load()
			duplicates, failOpen := s.metrics.snapshot()
			emit(consumerTick{
				envelopeHeader: envelopeHeader{Rig: rigName, Role: roleConsume, Kind: kindTick},
				ConsumerID:     s.cfg.ConsumerID,
				Delivered:      now,
				RateMsgPerSec:  float64(now-last) / s.cfg.TickEvery.Seconds(),
				Sequence:       s.seq.snapshot(),
				Guard:          s.ledger.snapshot(),
				GuardDupes:     duplicates,
				GuardFailOpen:  failOpen + s.failOpen(),
			})
			last = now
		}
	}
}

type consumerTick struct {
	envelopeHeader
	ConsumerID    string     `json:"consumer_id"`
	Delivered     int64      `json:"delivered"`
	RateMsgPerSec float64    `json:"msgs_per_sec"`
	Sequence      seqCounts  `json:"sequence"`
	Guard         guardStats `json:"guard"`
	GuardDupes    int64      `json:"guard_duplicate_verdicts"`
	GuardFailOpen int64      `json:"guard_fail_open"`
}

func (s *consumeState) summary() consumerSummary {
	duplicates, failOpen := s.metrics.snapshot()
	elapsed := max(time.Since(s.startedAt).Seconds(), 1)
	return consumerSummary{
		envelopeHeader:  envelopeHeader{Rig: rigName, Role: roleConsume, Kind: kindSummary},
		ConsumerID:      s.cfg.ConsumerID,
		Stream:          stressStreamName,
		Subject:         s.cfg.Subject,
		Group:           s.cfg.Group,
		ConsumeMode:     s.cfg.Mode,
		Routines:        s.cfg.Routines,
		GuardPrefix:     s.cfg.GuardPrefix,
		GuardTTL:        s.cfg.GuardTTL.String(),
		Seq:             s.seq.snapshot(),
		Guard:           s.ledger.flush(),
		GuardDuplicates: duplicates,
		GuardFailOpen:   failOpen + s.failOpen(),
		GuardLatency:    s.guardLatency.drain(),
		ValkeyOpsPerSec: float64(s.guardSampled.Load()) / elapsed,
		DecodeErrors:    s.decodeErrors.Load(),
		Lag:             s.lag.snapshot(),
		StartedAt:       s.startedAt,
		EndedAt:         time.Now(),
	}
}
