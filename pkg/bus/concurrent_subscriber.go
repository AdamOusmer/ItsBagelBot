package bus

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
	ackWait  time.Duration
	progress time.Duration
	log      *zap.Logger

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
)

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
		delay: cfg.delay, ackWait: 30 * time.Second, progress: time.Second, log: cfg.log,
		subs: make(map[*nats.Subscription]*callbackGate), closeCh: make(chan struct{}),
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
		select {
		case output <- msg:
			s.acks.Add(1)
			go s.awaitResult(natsMsg, msg)
		case <-ctx.Done():
		case <-s.closeCh:
		case <-callbacks.stopped():
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
	return newMessage(messageIdentity(wire), wire.Data, metadata), nil
}

func fleetMetadata(headers nats.Header) (Metadata, error) {
	metadata := make(Metadata, len(headers))
	for key, values := range headers {
		switch key {
		case MessageIDHeader, legacyMessageIDHeader,
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
	if id := wire.Header.Get(legacyMessageIDHeader); id != "" {
		return id
	}
	if metadata, err := wire.Metadata(); err == nil && metadata.Sequence.Stream > 0 {
		return fmt.Sprintf("js:%s:%s:%d", metadata.Domain, metadata.Stream, metadata.Sequence.Stream)
	}
	// This path covers legacy/core messages without JetStream reply metadata.
	// NUID is process-safe and avoids introducing UUID machinery.
	return nuid.Next()
}

func (s *concurrentDurableSubscriber) awaitResult(natsMsg *nats.Msg, msg *Message) {
	defer s.acks.Done()
	timer := time.NewTimer(s.ackWait)
	defer timer.Stop()
	progress := time.NewTicker(s.progress)
	defer progress.Stop()

	for {
		select {
		case <-msg.Acked():
			s.confirmAck(natsMsg)
			return
		case <-msg.Nacked():
			s.nack(natsMsg)
			return
		case <-timer.C:
			// The server's AckWait owns redelivery. Do not emit another NAK after the
			// deadline because it may already have redelivered to another replica.
			return
		case <-s.closeCh:
			return
		case <-progress.C:
			s.reportProgress(natsMsg)
		}
	}
}

func (s *concurrentDurableSubscriber) confirmAck(msg *nats.Msg) {
	// Double-ack so a successful return proves the consumer cursor advanced.
	// This network wait remains outside the serial subscription callback.
	if err := msg.AckSync(nats.AckWait(2 * time.Second)); err != nil {
		s.log.Warn("durable message confirmed ack failed; requesting replay", zap.String("subject", msg.Subject), zap.Error(err))
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
