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

	defaultPullAckWait       = 20 * time.Second
	defaultPullMaxAckPending = -1
	defaultPullNakDelay      = time.Second

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
		DeliverPolicy:     jsapi.DeliverAllPolicy,
		AckPolicy:         jsapi.AckExplicitPolicy,
		AckWait:           pullAckWait(),
		MaxDeliver:        -1,
		FilterSubject:     subject,
		ReplayPolicy:      jsapi.ReplayInstantPolicy,
		MaxAckPending:     pullMaxAckPending(),
		InactiveThreshold: time.Hour,
		Replicas:          pullReplicas(),
		MemoryStorage:     true,
		Metadata:          map[string]string{managedConsumerMetadata: "true", consumerContractMetadata: "2"},
	}
}

func PullLaneConsumerConfig(group, subject string) jsapi.ConsumerConfig {
	return pullConsumerConfig(subject, pullConsumerName(group, subject))
}

// No pod identity: a pod suffix silently turns distribution back into fan-out.
func pullConsumerName(group, subject string) string {
	return durableName(group, subject) + "_v2"
}

func pullAckWait() time.Duration {
	return positiveDuration(env.GetDuration("NATS_PULL_ACK_WAIT", defaultPullAckWait), defaultPullAckWait)
}

func pullNakDelay() time.Duration {
	return positiveDuration(env.GetDuration("NATS_PULL_NAK_DELAY", defaultPullNakDelay), defaultPullNakDelay)
}

func pullFetchMaxWait() time.Duration {
	return positiveDuration(env.GetDuration("NATS_PULL_FETCH_MAXWAIT", defaultPullFetchMaxWait), defaultPullFetchMaxWait)
}

func pullMaxAckPending() int {
	value := env.GetInt("NATS_PULL_MAX_ACK_PENDING", defaultPullMaxAckPending)
	if value == -1 {
		return value
	}
	return positiveInt(value, defaultPullMaxAckPending)
}

func pullFetchBatch() int {
	return positiveInt(env.GetInt("NATS_PULL_FETCH_BATCH", defaultPullFetchBatch), defaultPullFetchBatch)
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
	PauseConsumer(ctx context.Context, stream, name string, until time.Time) (*jsapi.ConsumerPauseResponse, error)
	DeleteConsumer(ctx context.Context, stream, name string) error
	Consumer(ctx context.Context, stream, name string) (jsapi.Consumer, error)
	PushConsumer(ctx context.Context, stream, name string) (jsapi.PushConsumer, error)
	CreateOrUpdateConsumer(ctx context.Context, stream string, cfg jsapi.ConsumerConfig) (jsapi.Consumer, error)
	CreateConsumer(ctx context.Context, stream string, cfg jsapi.ConsumerConfig) (jsapi.Consumer, error)
}

const pullLeaderPoll = 150 * time.Millisecond

type pullSubscriber struct {
	nc            *nats.Conn
	consumer      jsapi.Consumer
	desired       jsapi.ConsumerConfig
	rebind        func() (jsapi.Consumer, error)
	hot           *hotLaneBinding
	push          *pushLane
	extra         []*nats.Conn
	handles       []jsapi.Consumer
	handleMu      sync.Mutex
	stream        string
	subject       string
	name          string
	log           *zap.Logger
	letters       *concurrentDurableSubscriber
	pending       [32]pullPendingShard
	replyOnce     sync.Once
	replyPrefix   string
	ackMu         sync.Mutex
	expired       atomic.Uint64
	expiredUntil  atomic.Int64
	lagged        atomic.Bool
	horizon       atomic.Int64
	heartbeatStop chan struct{}
	heartbeatDone chan struct{}

	batch     int
	loops     int
	maxWait   time.Duration
	fetchErrs atomic.Int64
	ackErrs   atomic.Int64
	rebuilt   atomic.Int64
	closed    atomic.Bool

	errSince atomic.Int64

	consumerMu sync.Mutex

	logMu           sync.Mutex
	lastFetchErr    string
	lastFetchLog    time.Time
	lastProgressLog time.Time
	lastExpiredLog  time.Time

	output  chan *Message
	closeCh chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc

	workers  sync.WaitGroup
	inflight sync.WaitGroup
	once     sync.Once
	closeErr error
}

func newExplicitLaneSubscriber(cfg flowLaneConfig, mode laneConsumeMode) (*pullSubscriber, error) {
	nc, err := nats.Connect(busURL(endpoint(cfg.url)), busOptions(clientName(cfg.group+"-"+string(mode)))...)
	if err != nil {
		return nil, err
	}
	s, err := openExplicitLane(cfg, nc, mode)
	if err != nil {
		nc.Close()
	}
	return s, err
}

func openExplicitLane(cfg flowLaneConfig, nc *nats.Conn, mode laneConsumeMode) (*pullSubscriber, error) {
	desired := PullLaneConsumerConfig(cfg.group, cfg.subject)
	if mode == laneModePush {
		desired = pushConsumerConfig(cfg)
	}
	s, err := newPullSubscriber(cfg, nc, desired)
	if err != nil {
		return nil, err
	}
	if err := s.configureLane(cfg, mode); err != nil {
		return nil, err
	}
	s.consumer, err = s.rebind()
	if err != nil {
		return nil, err
	}
	if err := s.startSource(cfg); err != nil {
		return nil, err
	}
	s.start()
	return s, nil
}

func newPullSubscriber(cfg flowLaneConfig, nc *nats.Conn, desired jsapi.ConsumerConfig) (*pullSubscriber, error) {
	js, err := nc.JetStream(jsDomainOption()...)
	if err != nil {
		return nil, err
	}
	info, err := js.StreamInfo(cfg.stream)
	if err != nil {
		return nil, err
	}
	log := cfg.log
	if log == nil {
		log = zap.NewNop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &pullSubscriber{
		nc: nc, desired: desired,
		stream: cfg.stream, subject: cfg.subject,
		name: desired.Name, log: log,
		output:        make(chan *Message),
		closeCh:       make(chan struct{}),
		ctx:           ctx,
		cancel:        cancel,
		heartbeatStop: make(chan struct{}),
		heartbeatDone: make(chan struct{}),
	}
	s.horizon.Store(int64(info.Config.MaxAge))
	s.letters = &concurrentDurableSubscriber{nc: nc, js: js, stream: s.stream, consumer: s.name, log: log}
	return s, nil
}

func (s *pullSubscriber) configureLane(cfg flowLaneConfig, mode laneConsumeMode) error {
	if isHotIngressLane(cfg.stream, cfg.subject) {
		hot, err := newHotLane(s, cfg)
		if err != nil {
			return err
		}
		s.hot = hot
		s.rebind = hot.ensure
	} else {
		s.rebind = func() (jsapi.Consumer, error) { return ensurePullConsumer(s.nc, s.stream, s.desired) }
	}
	if mode == laneModePush {
		s.push = newPushLane(s)
		s.nc.SetErrorHandler(s.push.subscriptionError)
		return s.push.bind()
	}
	return nil
}

func (s *pullSubscriber) startSource(cfg flowLaneConfig) error {
	if s.push != nil {
		s.workers.Add(1)
		go s.push.pump()
		return nil
	}
	s.loops, s.batch, s.maxWait = pullFetchLoops(), pullFetchBatch(), pullFetchMaxWait()
	return s.openExtraConnections(cfg)
}

func (s *pullSubscriber) start() {
	s.workers.Add(s.loops)
	go s.heartbeat()
	for i := range s.loops {
		go s.pump(i)
	}
}

func (s *pullSubscriber) Subscribe(_ context.Context, subject string) (<-chan *Message, error) {
	if subject != s.subject {
		return nil, fmt.Errorf("bus: lane subscriber is bound to %q, not %q", s.subject, subject)
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
	if s.hot != nil && s.hot.paused.Load() {
		return s.pause(s.maxWait)
	}
	batch, err := s.handleFor(i).Fetch(s.batch, jsapi.FetchMaxWait(s.maxWait))
	if err != nil {
		return s.noteFetchError(err)
	}
	deliveries := make(chan jsapi.Msg, s.batch)
	collected := make(chan struct{})
	go s.collectBatch(batch, deliveries, collected)
	defer func() { <-collected }()
	s.noteFetchProgress()
	for wire := range deliveries {
		s.noteFetchProgress()
		if !s.deliver(wire, nil) {
			s.returnBatch(deliveries)
			return false
		}
	}
	if err := batch.Error(); err != nil {
		return s.noteFetchError(err)
	}
	return !s.closed.Load()
}

func (s *pullSubscriber) returnBatch(deliveries <-chan jsapi.Msg) {
	for wire := range deliveries {
		s.returnDelivery(wire)
	}
}

func (s *pullSubscriber) returnDelivery(wire jsapi.Msg) {
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	tracked := s.releaseDelivery(wire.Reply())
	if tracked || s.push != nil {
		_ = wire.Nak()
	}
}

func (s *pullSubscriber) collectBatch(batch jsapi.MessageBatch, deliveries chan<- jsapi.Msg, done chan<- struct{}) {
	defer close(done)
	defer close(deliveries)
	for wire := range batch.Messages() {
		s.trackDelivery(wire)
		deliveries <- wire
	}
}

func (s *pullSubscriber) boundConsumer() jsapi.Consumer {
	s.consumerMu.Lock()
	defer s.consumerMu.Unlock()
	return s.consumer
}

func (s *pullSubscriber) deliver(wire jsapi.Msg, shed <-chan struct{}) bool {
	if s.closed.Load() {
		s.returnDelivery(wire)
		return false
	}
	ticket := s.deliveryTicket(wire.Reply())
	if ticket == nil {
		return true
	}
	msg, err := s.decode(wire, ticket)
	if err != nil {
		s.beginDelivery()
		go func() {
			defer s.finishDelivery()
			s.quarantineWire(wire)
		}()
		return true
	}
	s.beginDelivery()
	c := msg.completionState()
	reply := wire.Reply()
	metadata, _ := wire.Metadata()
	c.subscriber = s
	c.ticket = ticket
	retryDelay := deliveryRetryDelay(metadata)
	msg.setResolveHandler(func(acked bool) {
		defer s.finishDelivery()
		s.resolveDelivery(reply, c, retryDelay, acked)
	})
	msg.reject = func(rejected *Message) error { return s.rejectDelivery(metadata, rejected) }
	select {
	case s.output <- msg:
		s.markHanded(ticket)
		return true
	case <-s.closeCh:
	case <-shed:
	}
	s.returnDelivery(wire)
	s.finishDelivery()
	return false
}

func (s *pullSubscriber) beginDelivery() {
	s.inflight.Add(1)
	if s.push != nil {
		s.push.active.Add(1)
	}
}

func (s *pullSubscriber) finishDelivery() {
	if s.push != nil {
		s.push.active.Add(-1)
	}
	s.inflight.Done()
}

func (s *pullSubscriber) decode(wire jsapi.Msg, ticket *DeliveryTicket) (*Message, error) {
	core := pullWireMessage(wire)
	msg, err := messageFromNATS(core)
	releasePullEnvelope(core)
	if err != nil {
		return nil, err
	}
	msg.SetContext(s.deliveryContext(ticket))
	msg.receivedAt = time.Unix(0, ticket.stored).Add(time.Duration(ticket.received) * time.Millisecond)
	msg.brokerTime = ticket.brokerTime
	msg.hasBrokerTime = true
	return msg, nil
}

var pullEnvelopePool = sync.Pool{New: func() any { return new(nats.Msg) }}

func releasePullEnvelope(core *nats.Msg) {
	*core = nats.Msg{}
	pullEnvelopePool.Put(core)
}

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

func (s *pullSubscriber) noteFetchError(err error) bool {
	if s.closed.Load() {
		return false
	}
	s.markLaneError()
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
	if err := s.rebindConsumer(); err != nil {
		s.log.Warn("lane consumer rebuild failed", zap.String("consumer", s.name), zap.Error(err))
		return
	}
	s.log.Warn("lane consumer was missing and has been rebuilt",
		zap.String("stream", s.stream),
		zap.String("subject", s.subject),
		zap.String("consumer", s.name),
		zap.Int64("rebuilds", s.rebuilt.Add(1)))
}

func (s *pullSubscriber) rebindConsumer() error {
	s.consumerMu.Lock()
	consumer, err := s.rebind()
	if err != nil {
		s.consumerMu.Unlock()
		return err
	}
	s.consumer = consumer
	s.consumerMu.Unlock()
	// Only after releasing consumerMu: handleFor takes handleMu then consumerMu.
	s.relookupHandles()
	return nil
}

func (s *pullSubscriber) Healthy() bool {
	if time.Now().UnixNano() < s.expiredUntil.Load() {
		return false
	}
	since := s.errSince.Load()
	withinGrace := since == 0 || time.Since(time.Unix(0, since)) < laneUnhealthyAfter
	return !s.lagged.Load() && withinGrace
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
	s.once.Do(func() { s.closeErr = s.shutdown() })
	return s.closeErr
}

func (s *pullSubscriber) shutdown() error {
	s.closed.Store(true)
	if s.cancel != nil {
		s.cancel()
	}
	close(s.closeCh)
	deadline := time.NewTimer(subscriberDrainTimeout)
	defer deadline.Stop()
	if s.push != nil {
		s.push.drain()
	}
	s.workers.Wait()
	drained := waitGroupBefore(&s.inflight, deadline.C)
	close(s.heartbeatStop)
	<-s.heartbeatDone

	err := s.flushAndClose(drained)
	close(s.output)
	return err
}

func (s *pullSubscriber) flushAndClose(drained bool) error {
	flushErr := s.nc.FlushTimeout(2 * time.Second)
	s.nc.Close()
	s.closeExtra()
	if !drained {
		return errors.New("bus: timed out draining lane deliveries")
	}
	if flushErr != nil {
		return fmt.Errorf("bus: flush lane acknowledgements: %w", flushErr)
	}
	return nil
}
