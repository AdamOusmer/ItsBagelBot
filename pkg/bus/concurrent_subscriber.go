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

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"go.uber.org/zap"
)

// concurrentDurableSubscriber is the fleet-owned JetStream delivery adapter.
// nats.go invokes one subscription callback serially, so the callback must not
// wait for the handler's acknowledgement: doing so hard-caps every pod at one in-flight
// event. This adapter hands the message to the weighted routine pool, returns
// immediately, and reconciles Ack/Nack concurrently after the handler finishes.
type concurrentDurableSubscriber struct {
	nc       *nats.Conn
	js       nats.JetStreamContext
	stream   string
	consumer string
	group    string
	delay    redeliveryDelay
	// handlerDeadline is the ceiling on one handler's total run time, NOT the
	// consumer's AckWait. Those are different clocks and used to share a name.
	// awaitResult reports InProgress every `progress`, which resets the server's
	// AckWait for as long as the handler lives, so the server does not redeliver
	// underneath a slow-but-healthy handler. This is what stops that from being
	// unbounded: past it the subscriber stops reporting progress and lets AckWait
	// take the message back.
	handlerDeadline time.Duration
	progress        time.Duration
	// ackSync confirms each acknowledgement with the server instead of firing it
	// asynchronously. It is set for work-queue retention only; see ack.
	ackSync bool
	log     *zap.Logger

	mu      sync.Mutex
	closed  bool
	subs    map[*nats.Subscription]*callbackGate
	closeCh chan struct{}

	registrations sync.WaitGroup
	acks          sync.WaitGroup
}

// callbackGate makes callback admission and shutdown one atomic decision.
// nats.go may invoke an async callback just after Unsubscribe returns, so a
// WaitGroup alone cannot safely guard channel closure: Add could race Wait.
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
	delay    redeliveryDelay
	log      *zap.Logger
}

const (
	terminateDelivery      = time.Duration(-1)
	subscriberDrainTimeout = 30 * time.Second
	// laneAckWait is the server-side redelivery clock every lane consumer is
	// created with (see laneConsumerConfig). It is named here because two things
	// in this file are sized against it and a literal in three places drifts.
	laneAckWait = 4 * time.Second
	// ackSyncTimeout bounds the confirmed acknowledgement on work-queue lanes. It
	// stays under laneAckWait so a failed confirmation still has room to
	// NAK before the server redelivers on its own clock.
	ackSyncTimeout = laneAckWait / 2
)

// workQueueRetention reports whether a catalog stream deletes messages on
// acknowledgement. The two acknowledgement contracts differ in what a lost ACK
// costs, so the subscriber picks its ack mode from the retention policy rather
// than from a caller-supplied flag that can drift from the catalog.
func workQueueRetention(stream string) bool {
	specs := make([]StreamSpec, 0, len(DataStreams)+2)
	specs = append(specs, DataStreams...)
	specs = append(specs, OutgressStream, OutgressSystemStream,
		YouTubeOutgressStream, DiscordOutgressStream)

	for _, spec := range specs {
		if spec.Name == stream {
			return spec.Retention == nats.WorkQueuePolicy
		}
	}
	return false
}

// redeliveryDelay keeps retry pacing behind the native subscriber abstraction.
// retry is JetStream's one-based NumDelivered counter.
type redeliveryDelay interface {
	WaitTime(retry uint64) time.Duration
}

type maxRetryDelay struct {
	delay time.Duration
	max   uint64
}

func newMaxRetryDelay(delay time.Duration, max uint64) maxRetryDelay {
	return maxRetryDelay{delay: delay, max: max}
}

func (d maxRetryDelay) WaitTime(retry uint64) time.Duration {
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
		subs:    make(map[*nats.Subscription]*callbackGate), closeCh: make(chan struct{}),
	}
	// Keep the WaitGroup positive until Close has unsubscribed every callback;
	// this prevents an Add racing a Wait while a final delivery is arriving.
	s.acks.Add(1)
	return s
}

func (s *concurrentDurableSubscriber) Subscribe(ctx context.Context, subject string) (<-chan *Message, error) {
	output := make(chan *Message)
	callbacks := newCallbackGate()

	if !s.beginRegistration() {
		return nil, errors.New("bus: subscriber is closed")
	}
	defer s.registrations.Done()

	callback := s.deliveryCallback(ctx, subject, output, callbacks)
	sub, err := s.subscribe(subject, callback)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		stopSubscription(sub, callbacks)
		return nil, errors.New("bus: subscriber closed during subscribe")
	}
	s.subs[sub] = callbacks
	s.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
		case <-s.closeCh:
		}
		stopSubscription(sub, callbacks)
		s.mu.Lock()
		delete(s.subs, sub)
		s.mu.Unlock()
		close(output)
	}()

	return output, nil
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
	// Broadcast subscriptions are ephemeral and start at messages published
	// after the binding. Each service instance owns a distinct consumer, so a
	// cache invalidation fans out to every replica.
	return s.js.Subscribe(subject, callback,
		nats.BindStream(s.stream), nats.DeliverNew(), nats.AckExplicit(), nats.ManualAck())
}

func (s *concurrentDurableSubscriber) deliveryCallback(
	ctx context.Context,
	subject string,
	output chan<- *Message,
	callbacks *callbackGate,
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
		// The result watch MUST be installed before the handoff. A fast
		// worker resolves the instant the send completes, and a handler
		// installed after that winning transition is never called — the ack
		// is silently dropped. This broker never fires AckWait redelivery
		// while interest stays bound, so every dropped slot strands one
		// MaxAckPending seat until max_age; that race is what capped the
		// lane near 2-3k msg/s. The non-delivery arms unwind the watch
		// themselves and resultWatch.finished makes double release a no-op.
		w := &resultWatch{s: s, natsMsg: natsMsg}
		w.timer = time.AfterFunc(s.progress, w.keepAlive)
		msg.setResolveHandler(w.resolve)
		s.acks.Add(1)
		handed := false
		select {
		case output <- msg:
			handed = true
		case <-ctx.Done():
		case <-s.closeCh:
		case <-callbacks.stopped():
		}
		if !handed {
			w.timer.Stop()
			if w.finished.CompareAndSwap(false, true) {
				s.acks.Done()
			}
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

func stopSubscription(sub *nats.Subscription, callbacks *callbackGate) {
	_ = sub.Unsubscribe()
	callbacks.stopAndWait()
}

func (s *concurrentDurableSubscriber) terminateMalformed(msg *nats.Msg, subject string, decodeErr error) {
	s.log.Warn("terminating malformed NATS delivery", zap.String("subject", subject), zap.Error(decodeErr))
	if err := msg.Term(); err != nil {
		s.log.Warn("malformed NATS delivery TERM failed", zap.String("subject", subject), zap.Error(err))
	}
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
	}), nil
}

func fleetMetadata(headers nats.Header) (Metadata, error) {
	metadata := make(Metadata, len(headers))
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
	// This path covers legacy/core messages without JetStream reply metadata.
	// NUID is process-safe and avoids introducing UUID machinery.
	return nuid.Next()
}

// jetStreamIdentity is the fallback identity for an event whose publisher set
// none. It is derived rather than random so it survives a retry hop: the pull
// adapter stamps it from the pull API's own metadata (see pullWireMessage),
// which cannot reach nats.go's subscription-bound parser, and both paths must
// produce the same string for the same delivery.
func jetStreamIdentity(domain, stream string, sequence uint64) string {
	return fmt.Sprintf("js:%s:%s:%d", domain, stream, sequence)
}

// watchResult reconciles one delivery without parking a goroutine on it.
//
// The resolve callback runs the acknowledgement on the worker goroutine that
// finished the handler, and an AfterFunc keepalive covers the slow-handler
// case: each fire reports InProgress (renewing the server's AckWait) until the
// coarse handlerDeadline, after which ownership is deliberately given up to
// AckWait redelivery. On the normal path the handler resolves inside one
// progress interval, so the whole mechanism costs one timer registration and
// one cancellation per message — no goroutine, no channel park. The finished
// flag is the single arbiter between a late resolve and the deadline: whoever
// swaps it first owns the acks slot, exactly one of them releases it.
func (s *concurrentDurableSubscriber) watchResult(natsMsg *nats.Msg, msg *Message) {
	w := &resultWatch{s: s, natsMsg: natsMsg}
	w.timer = time.AfterFunc(s.progress, w.keepAlive)
	msg.setResolveHandler(w.resolve)
}

type resultWatch struct {
	s       *concurrentDurableSubscriber
	natsMsg *nats.Msg
	timer   *time.Timer
	// elapsed is touched only inside keepAlive; AfterFunc serializes the
	// callback with its own Reset, so it needs no lock.
	elapsed  time.Duration
	finished atomic.Bool
}

func (w *resultWatch) resolve(acked bool) {
	w.timer.Stop()
	if !w.finished.CompareAndSwap(false, true) {
		// The deadline already surrendered this delivery to AckWait; another
		// ACK or NAK here would address a message the server may have
		// redelivered elsewhere.
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

func (w *resultWatch) keepAlive() {
	if w.finished.Load() {
		return
	}
	w.elapsed += w.s.progress
	if w.elapsed >= w.s.handlerDeadline {
		// The server's AckWait owns redelivery now. Do not emit another NAK
		// after the deadline because it may already have redelivered to
		// another replica, and reporting progress would renew the ownership
		// the deadline exists to give up.
		if w.finished.CompareAndSwap(false, true) {
			w.s.acks.Done()
		}
		return
	}
	w.s.reportProgress(w.natsMsg)
	w.timer.Reset(w.s.progress)
}

// ack applies the acknowledgement contract the stream's retention actually
// needs. On a limits/interest stream the ACK only advances a cursor, so it is
// fired asynchronously: the double-ack proved the cursor had moved at the cost
// of a round trip per message, which becomes a RAFT quorum round trip per
// message once the lane's stream is replicated. Handlers are idempotent by
// contract (ADR 0003), so a lost ACK there only risks one redelivery after
// AckWait, which is safe; a stalled quorum on every ACK is not.
func (s *concurrentDurableSubscriber) ack(msg *nats.Msg) {
	if s.ackSync {
		s.ackWorkQueue(msg)
		return
	}
	if err := msg.Ack(); err != nil {
		s.log.Warn("durable message ack failed; leaving redelivery to AckWait",
			zap.String("subject", msg.Subject), zap.Error(err))
	}
}

// ackWorkQueue confirms the acknowledgement reached the server. Work-queue
// retention deletes the message on ack, so an ACK that never lands is not a
// stale cursor but a redelivery of work that already ran — on TWITCH_OUTGRESS
// that is the same chat line sent twice. A failed confirmation NAKs instead, so
// the redelivery is deliberate and paced rather than an AckWait surprise; if the
// ACK did land and only its reply was lost, the NAK addresses a message the
// server has already removed and does nothing.
func (s *concurrentDurableSubscriber) ackWorkQueue(msg *nats.Msg) {
	if err := msg.AckSync(nats.AckWait(ackSyncTimeout)); err != nil {
		s.log.Warn("work-queue message ack was not confirmed; nacking to force a paced redelivery",
			zap.String("subject", msg.Subject), zap.Error(err))
		s.nack(msg)
	}
}

func (s *concurrentDurableSubscriber) reportProgress(msg *nats.Msg) {
	// Slow RPC-backed commands retain ownership. The normal path never reaches
	// this ticker because processing finishes in well under one second.
	if err := msg.InProgress(); err != nil {
		s.log.Warn("durable message progress ack failed", zap.String("subject", msg.Subject), zap.Error(err))
	}
}

func (s *concurrentDurableSubscriber) nack(msg *nats.Msg) {
	delay := time.Duration(0)
	if s.delay != nil {
		if metadata, err := msg.Metadata(); err == nil {
			delay = s.delay.WaitTime(metadata.NumDelivered)
		}
	}
	var err error
	switch {
	case delay == terminateDelivery:
		err = msg.Term()
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

	s.acks.Done() // no callback can Add after the drain barrier
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
}

func (s *concurrentDurableSubscriber) beginClose() ([]ownedSubscription, bool) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, false
	}
	s.closed = true
	s.mu.Unlock()

	// beginRegistration serializes Add with the closed flag under mu, so once
	// closed is set no registration can race this Wait.
	s.registrations.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	subs := make([]ownedSubscription, 0, len(s.subs))
	for sub, callbacks := range s.subs {
		subs = append(subs, ownedSubscription{sub: sub, callbacks: callbacks})
	}
	return subs, true
}

func (s *concurrentDurableSubscriber) stopCallbacks(subs []ownedSubscription) {
	for _, owned := range subs {
		if err := owned.sub.Unsubscribe(); err != nil && !errors.Is(err, nats.ErrBadSubscription) {
			s.log.Warn("durable subscription stop failed", zap.String("subject", owned.sub.Subject), zap.Error(err))
		}
		owned.callbacks.stopAndWait()
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
