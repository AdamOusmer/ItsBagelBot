// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"go.uber.org/zap"
)

type concurrentDurableSubscriber struct {
	nc              *nats.Conn
	js              nats.JetStreamContext
	stream          string
	consumer        string
	group           string
	delay           maxRetryDelay
	handlerDeadline time.Duration
	progress        time.Duration
	ackSync         bool
	log             *zap.Logger

	dropped atomic.Int64
	wheel   *keepAliveWheel

	mu      sync.Mutex
	closed  bool
	subs    map[*nats.Subscription]ownedSubscription
	closeCh chan struct{}

	registrations sync.WaitGroup
	acks          sync.WaitGroup
}

type callbackGate struct {
	mu       sync.Mutex
	stopping bool
	stopCh   chan struct{}
	active   sync.WaitGroup
}

type concurrentSubscriberConfig struct {
	nc       *nats.Conn
	js       nats.JetStreamContext
	stream   string
	consumer string
	group    string
	delay    maxRetryDelay
	log      *zap.Logger
}

const (
	terminateDelivery      = time.Duration(-1)
	subscriberDrainTimeout = 30 * time.Second
	laneAckWait            = 4 * time.Second
	ackSyncTimeout         = laneAckWait / 2

	durableQueueBytesBudget = 8 << 20

	durableWireBytesFloor = 1024

	durableQueueDepth = durableQueueBytesBudget / durableWireBytesFloor
)

func workQueueRetention(stream string) bool {
	specs := make([]StreamSpec, 0, len(DataStreams)+2)
	specs = append(specs, DataStreams...)
	specs = append(specs, OutgressStream, OutgressSystemStream)

	for _, spec := range specs {
		if spec.Name == stream {
			return spec.Retention == nats.WorkQueuePolicy
		}
	}
	return false
}

type maxRetryDelay struct {
	delay time.Duration
	max   uint64
}

func newMaxRetryDelay(delay time.Duration, max uint64) maxRetryDelay {
	return maxRetryDelay{delay: delay, max: max}
}

func (d maxRetryDelay) WaitTime(retry uint64) time.Duration {
	if d.max == 0 {
		return 0
	}
	if retry >= d.max {
		return terminateDelivery
	}
	return d.delay
}

func newConcurrentDurableSubscriber(cfg concurrentSubscriberConfig) *concurrentDurableSubscriber {
	if cfg.log == nil {
		cfg.log = zap.NewNop()
	}
	s := &concurrentDurableSubscriber{
		nc: cfg.nc, js: cfg.js, stream: cfg.stream, consumer: cfg.consumer, group: cfg.group,
		delay: cfg.delay, handlerDeadline: 30 * time.Second, progress: time.Second, log: cfg.log,
		ackSync: workQueueRetention(cfg.stream),
		subs:    make(map[*nats.Subscription]ownedSubscription), closeCh: make(chan struct{}),
	}
	s.acks.Add(1)
	s.wheel = newKeepAliveWheel(wheelStepCount(s.handlerDeadline, s.progress), s.progress, s.closeCh)
	return s
}

func wheelStepCount(deadline, progress time.Duration) int {
	if progress <= 0 || deadline <= 0 {
		return 1
	}
	steps := (deadline + progress - 1) / progress
	if steps < 1 {
		return 1
	}
	return int(steps)
}

func (s *concurrentDurableSubscriber) Subscribe(ctx context.Context, subject string) (<-chan *Message, error) {
	output := make(chan *Message)
	callbacks := newCallbackGate()
	pump := newSubscriptionPump()

	if !s.beginRegistration() {
		return nil, errors.New("bus: subscriber is closed")
	}
	defer s.registrations.Done()

	callback := s.deliveryCallback(ctx, subject, callbacks, pump.queue)
	sub, err := s.subscribe(subject, callback)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		stopSubscription(sub, callbacks, pump)
		return nil, errors.New("bus: subscriber closed during subscribe")
	}
	s.subs[sub] = ownedSubscription{sub: sub, callbacks: callbacks, pump: pump}
	s.mu.Unlock()

	go s.watchBind(ctx, sub, callbacks, pump)
	go s.runPump(ctx, output, callbacks, pump)

	return output, nil
}

func (s *concurrentDurableSubscriber) watchBind(ctx context.Context, sub *nats.Subscription, callbacks *callbackGate, pump *subscriptionPump) {
	select {
	case <-ctx.Done():
	case <-s.closeCh:
	}
	stopSubscription(sub, callbacks, pump)
	s.mu.Lock()
	delete(s.subs, sub)
	s.mu.Unlock()
}

func (s *concurrentDurableSubscriber) runPump(ctx context.Context, output chan<- *Message, callbacks *callbackGate, pump *subscriptionPump) {
	defer close(output)
	for live := true; live; {
		select {
		case d := <-pump.queue:
			live = s.deliver(ctx, output, callbacks, d)
		case <-pump.stop:
			live = false
		}
	}
	<-pump.stop
	s.abandon(pump.queue)
}

func (s *concurrentDurableSubscriber) beginRegistration() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.registrations.Add(1)
	return true
}

func (s *concurrentDurableSubscriber) subscribe(subject string, callback nats.MsgHandler) (*nats.Subscription, error) {
	if s.consumer != "" {
		return s.js.QueueSubscribe(subject, s.group, callback,
			nats.Bind(s.stream, s.consumer), nats.ManualAck())
	}
	return s.js.Subscribe(subject, callback,
		nats.BindStream(s.stream), nats.DeliverNew(), nats.AckExplicit(), nats.ManualAck())
}

type pendingDelivery struct {
	msg   *Message
	watch *resultWatch
}

type subscriptionPump struct {
	queue chan pendingDelivery
	stop  chan struct{}
	once  sync.Once
}

func newSubscriptionPump() *subscriptionPump {
	return &subscriptionPump{
		queue: make(chan pendingDelivery, durableQueueDepth),
		stop:  make(chan struct{}),
	}
}

// halt must run only after callbacks.stopAndWait, or the pump's final drain races an enqueuer.
func (p *subscriptionPump) halt() { p.once.Do(func() { close(p.stop) }) }

func (s *concurrentDurableSubscriber) deliveryCallback(
	ctx context.Context,
	subject string,
	callbacks *callbackGate,
	queue chan<- pendingDelivery,
) nats.MsgHandler {
	return func(natsMsg *nats.Msg) {
		if !callbacks.enter() {
			return
		}
		defer callbacks.leave()

		msg, err := messageFromNATS(natsMsg)
		if err != nil {
			s.terminateMalformed(natsMsg, subject, err)
			return
		}
		msg.SetContext(ctx)
		// The watch must exist before the handoff, or a fast worker's resolve drops the ack.
		w := s.newResultWatch(natsMsg, msg)
		select {
		case queue <- pendingDelivery{msg: msg, watch: w}:
		default:
			w.unwind()
			if dropped := s.dropped.Add(1); dropped == 1 || dropped%1_000 == 0 {
				s.log.Warn("durable delivery queue overflowed; leaving the message to AckWait redelivery",
					zap.String("subject", subject),
					zap.Int64("dropped_total", dropped))
			}
		}
	}
}

func (s *concurrentDurableSubscriber) newResultWatch(natsMsg *nats.Msg, msg *Message) *resultWatch {
	w := &resultWatch{s: s, natsMsg: natsMsg}
	// Add before registering: the wheel may release the seat as soon as the watch is registered.
	s.acks.Add(1)
	msg.setResolveHandler(w.resolve)
	s.wheel.register(w)
	return w
}

func (s *concurrentDurableSubscriber) deliver(
	ctx context.Context,
	output chan<- *Message,
	callbacks *callbackGate,
	d pendingDelivery,
) bool {
	select {
	case output <- d.msg:
		return true
	case <-ctx.Done():
	case <-s.closeCh:
	case <-callbacks.stopped():
	}
	d.watch.unwind()
	return false
}

func (s *concurrentDurableSubscriber) abandon(queue <-chan pendingDelivery) {
	for {
		select {
		case d := <-queue:
			d.watch.unwind()
		default:
			return
		}
	}
}

func newCallbackGate() *callbackGate {
	return &callbackGate{stopCh: make(chan struct{})}
}

func (g *callbackGate) enter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopping {
		return false
	}
	g.active.Add(1)
	return true
}

func (g *callbackGate) leave() { g.active.Done() }

func (g *callbackGate) stopped() <-chan struct{} { return g.stopCh }

func (g *callbackGate) stopAndWait() {
	g.mu.Lock()
	if !g.stopping {
		g.stopping = true
		close(g.stopCh)
	}
	g.mu.Unlock()
	g.active.Wait()
}

func stopSubscription(sub *nats.Subscription, callbacks *callbackGate, pump *subscriptionPump) {
	_ = sub.Unsubscribe()
	callbacks.stopAndWait()
	pump.halt()
}

func (s *concurrentDurableSubscriber) terminateMalformed(msg *nats.Msg, subject string, decodeErr error) {
	s.log.Warn("terminating malformed NATS delivery", zap.String("subject", subject), zap.Error(decodeErr))
	s.terminate(msg, deadLetterMalformed)
}

func messageFromNATS(wire *nats.Msg) (*Message, error) {
	metadata, err := fleetMetadata(wire.Header)
	if err != nil {
		return nil, err
	}
	return newMessage(messageData{
		id:       messageIdentity(wire),
		payload:  wire.Data,
		metadata: metadata,
		storedAt: jetStreamStoredAt(wire.Reply),
	}), nil
}

func jetStreamStoredAt(reply string) time.Time {
	if !strings.HasPrefix(reply, "$JS.ACK.") {
		return time.Time{}
	}
	tokens := strings.Split(reply, ".")
	var position int
	switch {
	case len(tokens) == 9:
		position = 7
	case len(tokens) >= 11:
		position = 9
	default:
		return time.Time{}
	}
	ns, err := strconv.ParseInt(tokens[position], 10, 64)
	if err != nil || ns <= 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func fleetMetadata(headers nats.Header) (Metadata, error) {
	var metadata Metadata
	for key, values := range headers {
		switch key {
		case MessageIDHeader,
			nats.MsgIdHdr, nats.ExpectedLastMsgIdHdr, nats.ExpectedStreamHdr,
			nats.ExpectedLastSubjSeqHdr, nats.ExpectedLastSeqHdr:
			continue
		}
		if len(values) != 1 {
			return nil, fmt.Errorf("bus: multiple values in NATS header %q: %v", key, values)
		}
		if metadata == nil {
			metadata = make(Metadata, len(headers))
		}
		metadata[key] = values[0]
	}
	return metadata, nil
}

func messageIdentity(wire *nats.Msg) string {
	if id := wire.Header.Get(MessageIDHeader); id != "" {
		return id
	}
	if metadata, err := wire.Metadata(); err == nil && metadata.Sequence.Stream > 0 {
		return jetStreamIdentity(metadata.Domain, metadata.Stream, metadata.Sequence.Stream)
	}
	return nuid.Next()
}

func jetStreamIdentity(domain, stream string, sequence uint64) string {
	return fmt.Sprintf("js:%s:%s:%d", domain, stream, sequence)
}

type resultWatch struct {
	s        *concurrentDurableSubscriber
	natsMsg  *nats.Msg
	armEpoch uint64
	finished atomic.Bool
}

func (w *resultWatch) unwind() {
	if w.finished.CompareAndSwap(false, true) {
		w.s.acks.Done()
	}
}

func (w *resultWatch) resolve(acked bool) {
	if !w.finished.CompareAndSwap(false, true) {
		return
	}
	defer w.s.acks.Done()
	select {
	case <-w.s.closeCh:
		return
	default:
	}
	if acked {
		w.s.ack(w.natsMsg)
		return
	}
	w.s.nack(w.natsMsg)
}

type keepAliveWheel struct {
	interval time.Duration
	steps    int

	mu      sync.Mutex
	buckets [][]*resultWatch
	cursor  int
	epoch   uint64

	done <-chan struct{}
}

func newKeepAliveWheel(steps int, interval time.Duration, done <-chan struct{}) *keepAliveWheel {
	if steps < 1 {
		steps = 1
	}
	w := &keepAliveWheel{
		interval: interval,
		steps:    steps,
		buckets:  make([][]*resultWatch, steps),
		done:     done,
	}
	go w.spin()
	return w
}

func (w *keepAliveWheel) register(watch *resultWatch) {
	w.mu.Lock()
	watch.armEpoch = w.epoch
	w.buckets[w.cursor] = append(w.buckets[w.cursor], watch)
	w.mu.Unlock()
}

func (w *keepAliveWheel) spin() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.advance()
		case <-w.done:
			return
		}
	}
}

func (w *keepAliveWheel) advance() {
	w.mu.Lock()
	due := w.buckets[w.cursor]
	w.buckets[w.cursor] = nil
	w.cursor = (w.cursor + 1) % len(w.buckets)
	w.epoch++
	e := w.epoch
	w.mu.Unlock()

	var survivors []*resultWatch
	for _, watch := range due {
		if w.sweepDue(e, watch) {
			survivors = append(survivors, watch)
		}
	}
	w.refile(survivors)
}

func (w *keepAliveWheel) sweepDue(epoch uint64, watch *resultWatch) bool {
	if watch.finished.Load() {
		return false
	}
	if epoch-watch.armEpoch < uint64(w.steps) {
		watch.s.reportProgress(watch.natsMsg)
		return true
	}
	if watch.finished.CompareAndSwap(false, true) {
		watch.s.acks.Done()
	}
	return false
}

// Re-read the bucket under the lock: the stale slice misses watches registered during the pass.
func (w *keepAliveWheel) refile(survivors []*resultWatch) {
	if len(survivors) == 0 {
		return
	}
	w.mu.Lock()
	w.buckets[w.cursor] = append(w.buckets[w.cursor], survivors...)
	w.mu.Unlock()
}

func (s *concurrentDurableSubscriber) ack(msg *nats.Msg) {
	// Work-queue acks must be confirmed: a lost ACK re-runs work that already ran.
	if s.ackSync {
		s.ackWorkQueue(msg)
		return
	}
	if err := msg.Ack(); err != nil {
		s.log.Warn("durable message ack failed; leaving redelivery to AckWait",
			zap.String("subject", msg.Subject), zap.Error(err))
	}
}

func (s *concurrentDurableSubscriber) ackWorkQueue(msg *nats.Msg) {
	if err := msg.AckSync(nats.AckWait(ackSyncTimeout)); err != nil {
		s.log.Warn("work-queue message ack was not confirmed; nacking to force a paced redelivery",
			zap.String("subject", msg.Subject), zap.Error(err))
		s.nack(msg)
	}
}

func (s *concurrentDurableSubscriber) reportProgress(msg *nats.Msg) {
	if err := msg.InProgress(); err != nil {
		s.log.Warn("durable message progress ack failed", zap.String("subject", msg.Subject), zap.Error(err))
	}
}

func (s *concurrentDurableSubscriber) nack(msg *nats.Msg) {
	delay := time.Duration(0)
	if metadata, err := msg.Metadata(); err == nil {
		delay = s.delay.WaitTime(metadata.NumDelivered)
	}
	if delay == terminateDelivery {
		s.terminate(msg, deadLetterMaxDeliveries)
		return
	}
	var err error
	switch {
	case delay > 0:
		err = msg.NakWithDelay(delay)
	default:
		err = msg.Nak()
	}
	if err != nil {
		s.log.Warn("durable message NAK failed", zap.String("subject", msg.Subject), zap.Error(err))
	}
}

func (s *concurrentDurableSubscriber) Close() error {
	subs, started := s.beginClose()
	if !started {
		return nil
	}

	deadline := time.NewTimer(subscriberDrainTimeout)
	defer deadline.Stop()
	s.stopCallbacks(subs)

	s.acks.Done()
	if !waitGroupBefore(&s.acks, deadline.C) {
		return s.abortClose(errors.New("bus: timed out draining durable acknowledgements"))
	}

	close(s.closeCh)
	if err := s.nc.FlushTimeout(2 * time.Second); err != nil {
		s.nc.Close()
		return fmt.Errorf("bus: flush subscriber acknowledgements: %w", err)
	}
	s.nc.Close()
	return nil
}

type ownedSubscription struct {
	sub       *nats.Subscription
	callbacks *callbackGate
	pump      *subscriptionPump
}

func (s *concurrentDurableSubscriber) beginClose() ([]ownedSubscription, bool) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, false
	}
	s.closed = true
	s.mu.Unlock()

	s.registrations.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	subs := make([]ownedSubscription, 0, len(s.subs))
	for _, owned := range s.subs {
		subs = append(subs, owned)
	}
	return subs, true
}

func (s *concurrentDurableSubscriber) stopCallbacks(subs []ownedSubscription) {
	for _, owned := range subs {
		if err := owned.sub.Unsubscribe(); err != nil && !errors.Is(err, nats.ErrBadSubscription) {
			s.log.Warn("durable subscription stop failed", zap.String("subject", owned.sub.Subject), zap.Error(err))
		}
		stopSubscription(owned.sub, owned.callbacks, owned.pump)
	}
}

func waitGroupBefore(group *sync.WaitGroup, deadline <-chan time.Time) bool {
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-deadline:
		return false
	}
}

func (s *concurrentDurableSubscriber) abortClose(err error) error {
	s.nc.Close()
	close(s.closeCh)
	return err
}
