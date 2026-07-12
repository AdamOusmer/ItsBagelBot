package bus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	wmnats "github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"
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
	delay    wmnats.Delay
	ackWait  time.Duration
	log      *zap.Logger

	mu      sync.Mutex
	closed  bool
	subs    map[*nats.Subscription]struct{}
	closeCh chan struct{}
	acks    sync.WaitGroup
}

func newConcurrentDurableSubscriber(
	nc *nats.Conn,
	js nats.JetStreamContext,
	stream, consumer, group string,
	delay wmnats.Delay,
	log *zap.Logger,
) *concurrentDurableSubscriber {
	if log == nil {
		log = zap.NewNop()
	}
	s := &concurrentDurableSubscriber{
		nc: nc, js: js, stream: stream, consumer: consumer, group: group,
		delay: delay, ackWait: 30 * time.Second, log: log,
		subs: make(map[*nats.Subscription]struct{}), closeCh: make(chan struct{}),
	}
	// Keep the WaitGroup positive until Close has unsubscribed every callback;
	// this prevents an Add racing a Wait while a final delivery is arriving.
	s.acks.Add(1)
	return s
}

func (s *concurrentDurableSubscriber) Subscribe(ctx context.Context, subject string) (<-chan *message.Message, error) {
	output := make(chan *message.Message)
	var deliveries sync.WaitGroup
	deliveries.Add(1) // callback-registration anchor; see goroutine below

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("bus: subscriber is closed")
	}
	s.mu.Unlock()

	unmarshaler := &wmnats.NATSMarshaler{}
	sub, err := s.js.QueueSubscribe(subject, s.group, func(natsMsg *nats.Msg) {
		deliveries.Add(1)
		defer deliveries.Done()

		msg, err := unmarshaler.Unmarshal(natsMsg)
		if err != nil {
			s.log.Warn("cannot decode durable NATS message", zap.String("subject", subject), zap.Error(err))
			return
		}
		select {
		case output <- msg:
			s.acks.Add(1)
			go s.awaitResult(natsMsg, msg)
		case <-ctx.Done():
		case <-s.closeCh:
		}
	}, nats.Bind(s.stream, s.consumer), nats.ManualAck())
	if err != nil {
		deliveries.Done()
		return nil, err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = sub.Unsubscribe()
		deliveries.Done()
		return nil, errors.New("bus: subscriber closed during subscribe")
	}
	s.subs[sub] = struct{}{}
	s.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
		case <-s.closeCh:
		}
		_ = sub.Unsubscribe()
		deliveries.Done()
		deliveries.Wait()
		s.mu.Lock()
		delete(s.subs, sub)
		s.mu.Unlock()
		close(output)
	}()

	return output, nil
}

func (s *concurrentDurableSubscriber) awaitResult(natsMsg *nats.Msg, msg *message.Message) {
	defer s.acks.Done()
	timer := time.NewTimer(s.ackWait)
	defer timer.Stop()

	select {
	case <-msg.Acked():
		// Ack is deliberately asynchronous. The handler has completed, and the
		// consumer contract is at-least-once: a disconnect that loses this Core
		// NATS ack correctly causes redelivery, where broker/output dedup folds it.
		// AckSync adds one JetStream round trip to every event and was the measured
		// delivery ceiling despite idle CPU.
		if err := natsMsg.Ack(); err != nil {
			s.log.Warn("durable message ack failed", zap.String("subject", natsMsg.Subject), zap.Error(err))
		}
	case <-msg.Nacked():
		s.nack(natsMsg)
	case <-timer.C:
		// The server's AckWait owns redelivery. Do not emit another NAK after the
		// deadline because it may already have redelivered to another replica.
	case <-s.closeCh:
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
	case delay == wmnats.TermSignal:
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
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	subs := make([]*nats.Subscription, 0, len(s.subs))
	for sub := range s.subs {
		subs = append(subs, sub)
	}
	s.mu.Unlock()

	for _, sub := range subs {
		// Drain waits for any callback that already entered before releasing the
		// Ack WaitGroup registration anchor below.
		_ = sub.Drain()
	}
	s.acks.Done() // release the registration anchor

	done := make(chan struct{})
	go func() {
		s.acks.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		close(s.closeCh)
		return errors.New("bus: timed out draining durable acknowledgements")
	}
	close(s.closeCh)
	if err := s.nc.Drain(); err != nil {
		return fmt.Errorf("bus: drain durable subscriber: %w", err)
	}
	return nil
}
