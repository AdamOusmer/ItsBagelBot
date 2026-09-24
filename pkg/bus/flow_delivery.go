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
	"go.uber.org/zap"
)

const (
	flowControlPendingBytes = 32 << 20

	flowWireBytesFloor = 800

	// Derived from the byte window: a shallower queue silently drops healthy deliveries.
	flowQueueDepth = flowControlPendingBytes / flowWireBytesFloor
)

type flowLaneConfig struct {
	url     string
	stream  string
	subject string
	group   string
	log     *zap.Logger
}

type flowDelivery struct {
	wire *nats.Msg
	msg  *Message
}

type flowSubscriber struct {
	nc             *nats.Conn
	stream         string
	subject        string
	group          string
	consumer       string
	deliverSubject string
	log            *zap.Logger

	cursor      flowCursor
	dropped     atomic.Int64
	retried     atomic.Int64
	stale       atomic.Int64
	overrun     atomic.Int64
	stallStreak atomic.Int64
	wedged      atomic.Int64
	lastControl atomic.Int64
	recovering  atomic.Bool
	closed      atomic.Bool

	sub     *nats.Subscription
	queue   chan flowDelivery
	output  chan *Message
	closeCh chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc

	workers sync.WaitGroup
	pending sync.WaitGroup
	once    sync.Once
}

func (s *flowSubscriber) binding() laneBinding {
	return laneBinding{stream: s.stream, subject: s.subject, consumer: s.consumer}
}

func newFlowLaneSubscriber(cfg flowLaneConfig) (*flowSubscriber, error) {
	lane := laneBinding{
		stream:   cfg.stream,
		subject:  cfg.subject,
		consumer: flowConsumerName(cfg.group, cfg.subject),
	}

	nc, err := nats.Connect(busURL(endpoint(cfg.url)), busOptions(clientName(cfg.group)+"-flow")...)
	if err != nil {
		return nil, err
	}
	deliver, err := ensureFlowConsumer(nc, lane, flowConsumerConfig(lane))
	if err != nil {
		nc.Close()
		return nil, err
	}

	s := newFlowSubscriber(cfg, nc, lane, deliver)
	if err := s.start(); err != nil {
		nc.Close()
		return nil, err
	}
	return s, nil
}

func newFlowSubscriber(cfg flowLaneConfig, nc *nats.Conn, lane laneBinding, deliver string) *flowSubscriber {
	log := cfg.log
	if log == nil {
		log = zap.NewNop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &flowSubscriber{
		nc: nc, stream: lane.stream, subject: lane.subject, group: cfg.group,
		consumer: lane.consumer, deliverSubject: deliver, log: log,
		queue:   make(chan flowDelivery, flowQueueDepth),
		output:  make(chan *Message),
		closeCh: make(chan struct{}),
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (s *flowSubscriber) start() error {
	sub, err := s.nc.Subscribe(s.deliverSubject, s.deliveryCallback)
	if err != nil {
		return err
	}
	s.sub = sub
	s.lastControl.Store(time.Now().UnixNano())
	s.workers.Add(2)
	go s.pump()
	go s.watchHeartbeats()
	return nil
}

func (s *flowSubscriber) Subscribe(_ context.Context, subject string) (<-chan *Message, error) {
	if subject != s.subject {
		return nil, fmt.Errorf("bus: flow subscriber is bound to %q, not %q", s.subject, subject)
	}
	if s.closed.Load() {
		return nil, errors.New("bus: subscriber is closed")
	}
	return s.output, nil
}

// Answer status messages before anything that can block, or the consumer's window closes.
func (s *flowSubscriber) deliveryCallback(wire *nats.Msg) {
	if status := wire.Header.Get(statusHeader); status != "" {
		s.handleStatus(wire, status)
		return
	}
	s.enqueue(wire)
}

func (s *flowSubscriber) enqueue(wire *nats.Msg) {
	s.stallStreak.Store(0)
	s.recordReceipt(wire)
	msg, err := messageFromNATS(wire)
	if err != nil {
		s.log.Warn("dropping malformed lane delivery", zap.String("subject", s.subject), zap.Error(err))
		return
	}
	msg.SetContext(s.laneContext())
	select {
	case s.queue <- flowDelivery{wire: wire, msg: msg}:
	default:
		if overrun := s.overrun.Add(1); overrun == 1 || overrun%1_000 == 0 {
			s.log.Warn("lane receipt queue overflowed",
				zap.String("subject", s.subject),
				zap.Int64("overrun_total", overrun))
		}
	}
}

func (s *flowSubscriber) laneContext() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

func (s *flowSubscriber) recordReceipt(wire *nats.Msg) {
	metadata, err := wire.Metadata()
	if err != nil {
		return
	}
	if s.cursor.record(metadata.Sequence.Consumer, metadata.Sequence.Stream) {
		return
	}
	if stale := s.stale.Add(1); stale == 1 || stale%10_000 == 0 {
		s.log.Warn("receipt cursor is not advancing under delivery",
			zap.String("subject", s.subject),
			zap.Uint64("delivery_stream_seq", metadata.Sequence.Stream),
			zap.Int64("stale_total", stale))
	}
}

func (s *flowSubscriber) pump() {
	defer s.workers.Done()
	for {
		select {
		case delivery := <-s.queue:
			if !s.deliver(delivery) {
				return
			}
		case <-s.closeCh:
			return
		}
	}
}

func (s *flowSubscriber) deliver(delivery flowDelivery) bool {
	// Count and resolve handler must be in place before any handler can see the message.
	s.pending.Add(1)
	delivery.msg.setResolveHandler(func(acked bool) {
		defer s.pending.Done()
		if !acked {
			s.scheduleRetry(delivery)
		}
	})
	select {
	case s.output <- delivery.msg:
		return true
	case <-s.closeCh:
		s.pending.Done()
		return false
	}
}

func (s *flowSubscriber) Close() error {
	var err error
	s.once.Do(func() { err = s.shutdown() })
	return err
}

func (s *flowSubscriber) shutdown() error {
	s.closed.Store(true)
	if s.cancel != nil {
		s.cancel()
	}
	if s.sub != nil {
		if unsub := s.sub.Unsubscribe(); unsub != nil && !errors.Is(unsub, nats.ErrBadSubscription) {
			s.log.Warn("flow subscription stop failed", zap.String("subject", s.subject), zap.Error(unsub))
		}
	}
	close(s.closeCh)
	s.workers.Wait()

	deadline := time.NewTimer(subscriberDrainTimeout)
	defer deadline.Stop()
	drained := waitGroupBefore(&s.pending, deadline.C)

	err := s.flushAndClose(drained)
	close(s.output)
	return err
}

func (s *flowSubscriber) flushAndClose(drained bool) error {
	flushErr := s.nc.FlushTimeout(2 * time.Second)
	s.nc.Close()
	if !drained {
		return errors.New("bus: timed out draining flow lane deliveries")
	}
	if flushErr != nil {
		return fmt.Errorf("bus: flush flow lane acknowledgements: %w", flushErr)
	}
	return nil
}
