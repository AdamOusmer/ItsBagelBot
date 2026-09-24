// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

const (
	defaultPullFetchBatch = 1000

	defaultPullFetchLoops = 1

	defaultPullFetchMaxWait = 200 * time.Millisecond

	// Must stay well above defaultPullAckEvery, or a batch mid-dispatch is redelivered to another pod.
	defaultPullAckWait = 30 * time.Second

	// Fleet-wide: must exceed pods x fetch loops x fetch batch.
	defaultPullMaxAckPending = 50_000

	defaultPullAckEvery = 250 * time.Millisecond

	defaultPullReplicas = 3

	laneUnhealthyAfter = 30 * time.Second
	fetchErrorLogEvery = time.Minute

	pullProvisionTimeout = 5 * time.Second
)

func pullConsumerConfig(subject, name string) jsapi.ConsumerConfig {
	return jsapi.ConsumerConfig{
		Name:              name,
		Durable:           name,
		Description:       "ItsBagelBot shared-durable pull lane consumer",
		DeliverPolicy:     jsapi.DeliverNewPolicy,
		AckPolicy:         pullAckPolicy(),
		AckWait:           pullAckWait(),
		MaxDeliver:        -1,
		FilterSubject:     subject,
		ReplayPolicy:      jsapi.ReplayInstantPolicy,
		MaxAckPending:     pullMaxAckPendingFor(pullAckPolicy()),
		InactiveThreshold: flowInactiveThreshold,
		Replicas:          pullReplicas(),
		MemoryStorage:     true,
		Metadata:          map[string]string{managedConsumerMetadata: "true"},
	}
}

// No pod identity: a pod suffix silently turns distribution back into fan-out.
func pullConsumerName(group, subject string) string {
	return durableName(group, subject)
}

func pullAckPolicy() jsapi.AckPolicy {
	if env.Get("NATS_PULL_ACK_POLICY", "none") == "all" {
		return jsapi.AckAllPolicy
	}
	return jsapi.AckNonePolicy
}

func pullMaxAckPendingFor(policy jsapi.AckPolicy) int {
	if policy == jsapi.AckNonePolicy {
		return 0
	}
	return pullMaxAckPending()
}

func pullAckWait() time.Duration {
	return positiveDuration(env.GetDuration("NATS_PULL_ACK_WAIT", defaultPullAckWait), defaultPullAckWait)
}

func pullAckEvery() time.Duration {
	return positiveDuration(env.GetDuration("NATS_PULL_ACK_EVERY", defaultPullAckEvery), defaultPullAckEvery)
}

func pullFetchMaxWait() time.Duration {
	return positiveDuration(env.GetDuration("NATS_PULL_FETCH_MAXWAIT", defaultPullFetchMaxWait), defaultPullFetchMaxWait)
}

func pullMaxAckPending() int {
	return positiveInt(env.GetInt("NATS_PULL_MAX_ACK_PENDING", defaultPullMaxAckPending), defaultPullMaxAckPending)
}

func pullFetchBatch() int {
	return positiveInt(env.GetInt("NATS_PULL_FETCH_BATCH", defaultPullFetchBatch), defaultPullFetchBatch)
}

func pullFetchExpiry() time.Duration {
	return positiveDuration(env.GetDuration("NATS_PULL_EXPIRY", 30*time.Second), 30*time.Second)
}

func pullFetchLoops() int {
	return positiveInt(env.GetInt("NATS_PULL_FETCH_LOOPS", defaultPullFetchLoops), defaultPullFetchLoops)
}

func pullReplicas() int {
	return positiveInt(env.GetInt("NATS_PULL_REPLICAS", defaultPullReplicas), defaultPullReplicas)
}

func positiveDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func positiveInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

type pullConsumerProvisioner interface {
	Consumer(ctx context.Context, stream, name string) (jsapi.Consumer, error)
	PushConsumer(ctx context.Context, stream, name string) (jsapi.PushConsumer, error)
	CreateOrUpdateConsumer(ctx context.Context, stream string, cfg jsapi.ConsumerConfig) (jsapi.Consumer, error)
	DeleteConsumer(ctx context.Context, stream, name string) error
}

const pullLeaderPoll = 150 * time.Millisecond

type pullDelivery struct {
	wire *nats.Msg
	msg  *Message
}

type pullSubscriber struct {
	nc       *nats.Conn
	consumer jsapi.Consumer
	desired  jsapi.ConsumerConfig
	rebind   func() (jsapi.Consumer, error)
	extra    []*nats.Conn
	handles  []jsapi.Consumer
	handleMu sync.Mutex
	stream   string
	subject  string
	name     string
	log      *zap.Logger

	batch    int
	loops    int
	maxWait  time.Duration
	ackEvery time.Duration

	retried   atomic.Int64
	dropped   atomic.Int64
	fetchErrs atomic.Int64
	ackErrs   atomic.Int64
	rebuilt   atomic.Int64
	closed    atomic.Bool

	errSince atomic.Int64

	ackMu   sync.Mutex
	pending jsapi.Msg

	consumerMu sync.Mutex

	logMu        sync.Mutex
	lastFetchErr string
	lastFetchLog time.Time

	output  chan *Message
	closeCh chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc

	workers  sync.WaitGroup
	inflight sync.WaitGroup
	once     sync.Once
}

func newPullLaneSubscriber(cfg flowLaneConfig) (*pullSubscriber, error) {
	name := pullConsumerName(cfg.group, cfg.subject)

	nc, err := nats.Connect(busURL(endpoint(cfg.url)), busOptions(clientName(cfg.group)+"-pull")...)
	if err != nil {
		return nil, err
	}
	consumer, err := ensurePullConsumer(nc, cfg.stream, pullConsumerConfig(cfg.subject, name))
	if err != nil {
		nc.Close()
		return nil, err
	}
	s := newPullSubscriber(cfg, nc, consumer, name)
	if err := s.openExtraConnections(cfg); err != nil {
		nc.Close()
		return nil, err
	}
	s.start()
	return s, nil
}

func newPullSubscriber(cfg flowLaneConfig, nc *nats.Conn, consumer jsapi.Consumer, name string) *pullSubscriber {
	log := cfg.log
	if log == nil {
		log = zap.NewNop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	loops, batch := pullFetchLoops(), pullFetchBatch()
	s := &pullSubscriber{
		nc: nc, consumer: consumer, desired: pullConsumerConfig(cfg.subject, name),
		stream: cfg.stream, subject: cfg.subject,
		name: name, log: log,
		batch: batch, loops: loops, maxWait: pullFetchMaxWait(), ackEvery: pullAckEvery(),
		output:  make(chan *Message, loops*batch),
		closeCh: make(chan struct{}),
		ctx:     ctx,
		cancel:  cancel,
	}
	s.rebind = func() (jsapi.Consumer, error) { return ensurePullConsumer(s.nc, s.stream, s.desired) }
	return s
}

func (s *pullSubscriber) start() {
	s.workers.Add(s.loops + 1)
	for i := range s.loops {
		go s.pump(i)
	}
	go s.advanceFloorPeriodically()
}

func (s *pullSubscriber) Subscribe(_ context.Context, subject string) (<-chan *Message, error) {
	if subject != s.subject {
		return nil, fmt.Errorf("bus: pull subscriber is bound to %q, not %q", s.subject, subject)
	}
	if s.closed.Load() {
		return nil, errors.New("bus: subscriber is closed")
	}
	return s.output, nil
}

func (s *pullSubscriber) pump(i int) {
	defer s.workers.Done()
	for s.pumpIterator(i) {
	}
}

func (s *pullSubscriber) pumpIterator(i int) bool {
	iter, err := s.handleFor(i).Messages(jsapi.PullMaxMessages(s.batch), jsapi.PullExpiry(pullFetchExpiry()))
	if err != nil {
		return s.noteFetchError(err)
	}
	release := s.stopIteratorOnClose(iter)
	defer release()
	s.noteFetchProgress()
	for {
		wire, err := iter.Next()
		if err != nil {
			s.advanceFloor()
			if s.closed.Load() {
				return false
			}
			return s.noteFetchError(err)
		}
		s.noteFetchProgress()
		if !s.deliver(wire) {
			return false
		}
	}
}

func (s *pullSubscriber) boundConsumer() jsapi.Consumer {
	s.consumerMu.Lock()
	defer s.consumerMu.Unlock()
	return s.consumer
}

func (s *pullSubscriber) stopIteratorOnClose(iter jsapi.MessagesContext) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-s.closeCh:
		case <-done:
		}
		iter.Stop()
	}()
	return func() { close(done) }
}

func (s *pullSubscriber) deliver(wire jsapi.Msg) bool {
	delivery, ok := s.decode(wire)
	if !ok {
		// Record malformed payloads too, or the floor holds a hole open for every pod.
		s.noteReceipt(wire)
		return true
	}
	// Count and resolve handler must be in place before any handler can see the message.
	s.inflight.Add(1)
	delivery.msg.setResolveHandler(func(acked bool) {
		defer s.inflight.Done()
		defer pullEnvelopePool.Put(delivery.wire)
		if !acked {
			s.scheduleRetry(delivery)
		}
	})
	select {
	case s.output <- delivery.msg:
		s.noteReceipt(wire)
		return true
	case <-s.closeCh:
		s.inflight.Done()
		pullEnvelopePool.Put(delivery.wire)
		return false
	}
}

func (s *pullSubscriber) decode(wire jsapi.Msg) (pullDelivery, bool) {
	core := pullWireMessage(wire)
	msg, err := messageFromNATS(core)
	if err != nil {
		pullEnvelopePool.Put(core)
		s.log.Warn("dropping malformed lane delivery",
			zap.String("subject", s.subject), zap.Error(err))
		return pullDelivery{}, false
	}
	msg.SetContext(s.laneContext())
	return pullDelivery{wire: core, msg: msg}, true
}

// Never clear Data on Put: a delivered Message may still alias it.
var pullEnvelopePool = sync.Pool{New: func() any { return new(nats.Msg) }}

func pullWireMessage(wire jsapi.Msg) *nats.Msg {
	core := pullEnvelopePool.Get().(*nats.Msg)
	core.Subject = wire.Subject()
	core.Reply = wire.Reply()
	core.Data = wire.Data()
	core.Header = wire.Headers()
	if core.Header == nil {
		core.Header = make(nats.Header, 1)
	}
	metadata, err := wire.Metadata()
	if err != nil || metadata.Sequence.Stream == 0 {
		return core
	}
	if core.Header.Get(MessageIDHeader) == "" {
		core.Header.Set(MessageIDHeader,
			jetStreamIdentity(metadata.Domain, metadata.Stream, metadata.Sequence.Stream))
	}
	return core
}

func (s *pullSubscriber) laneContext() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

func (s *pullSubscriber) noteReceipt(wire jsapi.Msg) {
	if s.desired.AckPolicy == jsapi.AckNonePolicy {
		return
	}
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	s.pending = wire
}

func (s *pullSubscriber) advanceFloor() {
	wire := s.takePending()
	if wire == nil {
		return
	}
	if err := wire.Ack(); err != nil {
		if count := s.ackErrs.Add(1); count == 1 || count%1_000 == 0 {
			s.log.Warn("lane floor ack failed",
				zap.String("subject", s.subject),
				zap.Int64("ack_errors", count),
				zap.Error(err))
		}
	}
}

func (s *pullSubscriber) takePending() jsapi.Msg {
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	wire := s.pending
	s.pending = nil
	return wire
}

func (s *pullSubscriber) advanceFloorPeriodically() {
	defer s.workers.Done()
	ticker := time.NewTicker(s.ackEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.closeCh:
			return
		case <-ticker.C:
			s.advanceFloor()
		}
	}
}

func (s *pullSubscriber) scheduleRetry(delivery pullDelivery) {
	if err := scheduleLaneRetry(s.nc, s.subject, delivery.wire, delivery.msg); err != nil {
		s.log.Warn("dropping failed lane event",
			zap.String("subject", s.subject),
			zap.String("message_id", delivery.msg.UUID),
			zap.Int64("dropped_total", s.dropped.Add(1)),
			zap.Error(err))
		return
	}
	s.retried.Add(1)
}

func (s *pullSubscriber) noteFetchError(err error) bool {
	if s.closed.Load() {
		return false
	}
	s.errSince.CompareAndSwap(0, time.Now().UnixNano())
	count := s.fetchErrs.Add(1)
	if s.shouldLogFetchError(err) {
		s.log.Warn("lane fetch failed",
			zap.String("stream", s.stream),
			zap.String("subject", s.subject),
			zap.String("consumer", s.name),
			zap.Int64("fetch_errors", count),
			zap.Error(err))
	}
	if consumerGone(err) {
		s.rebuildConsumer()
	}
	return s.pause(s.maxWait)
}

func (s *pullSubscriber) shouldLogFetchError(err error) bool {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	now, text := time.Now(), err.Error()
	if text == s.lastFetchErr && now.Sub(s.lastFetchLog) < fetchErrorLogEvery {
		return false
	}
	s.lastFetchErr, s.lastFetchLog = text, now
	return true
}

func (s *pullSubscriber) noteFetchProgress() {
	if s.errSince.Load() != 0 {
		s.errSince.Store(0)
	}
}

func consumerGone(err error) bool {
	return errors.Is(err, jsapi.ErrConsumerNotFound) || errors.Is(err, jsapi.ErrConsumerDeleted)
}

func (s *pullSubscriber) rebuildConsumer() {
	s.takePending()

	s.consumerMu.Lock()
	consumer, err := s.rebind()
	if err != nil {
		s.consumerMu.Unlock()
		s.log.Warn("lane consumer rebuild failed",
			zap.String("stream", s.stream),
			zap.String("subject", s.subject),
			zap.String("consumer", s.name),
			zap.Error(err))
		return
	}
	s.consumer = consumer
	s.consumerMu.Unlock()
	// Only after releasing consumerMu: handleFor takes handleMu then consumerMu.
	s.relookupHandles()
	s.log.Warn("lane consumer was missing and has been rebuilt",
		zap.String("stream", s.stream),
		zap.String("subject", s.subject),
		zap.String("consumer", s.name),
		zap.Int64("rebuilds", s.rebuilt.Add(1)))
}

func (s *pullSubscriber) Healthy() bool {
	since := s.errSince.Load()
	return since == 0 || time.Since(time.Unix(0, since)) < laneUnhealthyAfter
}

func (s *pullSubscriber) pause(wait time.Duration) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-s.closeCh:
		return false
	}
}

func (s *pullSubscriber) Close() error {
	var err error
	s.once.Do(func() { err = s.shutdown() })
	return err
}

func (s *pullSubscriber) shutdown() error {
	s.closed.Store(true)
	if s.cancel != nil {
		s.cancel()
	}
	close(s.closeCh)
	s.workers.Wait()
	s.advanceFloor()

	deadline := time.NewTimer(subscriberDrainTimeout)
	defer deadline.Stop()
	drained := waitGroupBefore(&s.inflight, deadline.C)

	err := s.flushAndClose(drained)
	close(s.output)
	return err
}

func (s *pullSubscriber) flushAndClose(drained bool) error {
	flushErr := s.nc.FlushTimeout(2 * time.Second)
	s.nc.Close()
	s.closeExtra()
	if !drained {
		return errors.New("bus: timed out draining pull lane deliveries")
	}
	if flushErr != nil {
		return fmt.Errorf("bus: flush pull lane acknowledgements: %w", flushErr)
	}
	return nil
}
