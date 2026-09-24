// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"go.uber.org/zap"

	"ItsBagelBot/pkg/env"
)

func NewSubscriber(url string, group string, log *zap.Logger) (Subscriber, error) {
	return newSubscriber(url, group, log)
}

const (
	fleetMaxRedeliveries uint64 = 5
	fleetNakDelay               = 3 * time.Second
)

type LaneConfig struct {
	URL             string
	Stream          string
	Subject         string
	Group           string
	NakDelay        time.Duration
	MaxRedeliveries uint64
}

func NewLaneSubscriber(cfg LaneConfig, log *zap.Logger) (Subscriber, error) {
	maxDeliveries := cfg.MaxRedeliveries + 1
	sub, _, err := bindDurable(cfg, int(maxDeliveries), newMaxRetryDelay(cfg.NakDelay, maxDeliveries), log)
	return sub, err
}

func bindDurable(cfg LaneConfig, maxDeliveries int, nakDelay maxRetryDelay, log *zap.Logger) (Subscriber, *nats.Conn, error) {
	consumer := durableName(cfg.Group, cfg.Subject)

	nc, err := nats.Connect(busURL(endpoint(cfg.URL)), busOptions(clientName(cfg.Group))...)
	if err != nil {
		return nil, nil, err
	}

	js, err := nc.JetStream(jsDomainOption()...)
	if err != nil {
		nc.Close()
		return nil, nil, err
	}
	// Provision explicitly: a consumer nats.go creates is deleted on unsubscribe, erasing the ack floor.
	if err := ensureConsumer(js, cfg.Stream, laneConsumerConfig(cfg.Subject, cfg.Group, consumer, maxDeliveries)); err != nil {
		nc.Close()
		return nil, nil, err
	}

	sub := newConcurrentDurableSubscriber(concurrentSubscriberConfig{
		nc: nc, js: js, stream: cfg.Stream, consumer: consumer,
		group: cfg.Group, delay: nakDelay, log: log,
	})
	return sub, nc, nil
}

const managedConsumerMetadata = "itsbagelbot.dev/managed"

func ensureConsumer(js nats.JetStreamManager, stream string, desired *nats.ConsumerConfig) error {
	info, err := js.ConsumerInfo(stream, desired.Name)
	if errors.Is(err, nats.ErrConsumerNotFound) {
		_, err = js.AddConsumer(stream, desired)
		return err
	}
	if err != nil {
		return err
	}

	desired.DeliverSubject = info.Config.DeliverSubject
	desired.DeliverPolicy = info.Config.DeliverPolicy
	desired.OptStartSeq = info.Config.OptStartSeq
	if _, err := js.UpdateConsumer(stream, desired); err != nil {
		// Only an immutable-field rejection may delete: a timeout fails too, and deleting replays retained work.
		if !requiresConsumerReplacement(err) {
			return fmt.Errorf("bus: update consumer %q: %w", desired.Name, err)
		}

		carryAckFloor(desired, info)
		return replaceConsumer(js, stream, desired, err)
	}
	return nil
}

func replaceConsumer(js nats.JetStreamManager, stream string, desired *nats.ConsumerConfig, cause error) error {
	if derr := js.DeleteConsumer(stream, desired.Name); derr != nil && !errors.Is(derr, nats.ErrConsumerNotFound) {
		return fmt.Errorf("bus: update consumer %q: %w (replace failed: %v)", desired.Name, cause, derr)
	}
	if _, aerr := js.AddConsumer(stream, desired); aerr != nil {
		return fmt.Errorf("bus: recreate consumer %q: %w", desired.Name, aerr)
	}
	return nil
}

func carryAckFloor(desired *nats.ConsumerConfig, info *nats.ConsumerInfo) {
	if info == nil || info.AckFloor.Stream == 0 {
		return
	}
	desired.DeliverPolicy = nats.DeliverByStartSequencePolicy
	desired.OptStartSeq = info.AckFloor.Stream + 1
}

// No BackOff: the server clamps AckWait to backoff[0] and redelivers slow handlers elsewhere.
func laneConsumerConfig(subject, group, name string, maxDeliveries int) *nats.ConsumerConfig {
	return &nats.ConsumerConfig{
		Durable:        name,
		Name:           name,
		Description:    "ItsBagelBot bounded work-queue lane consumer",
		DeliverPolicy:  nats.DeliverAllPolicy,
		AckPolicy:      nats.AckExplicitPolicy,
		AckWait:        laneAckWait,
		MaxDeliver:     maxDeliveries,
		FilterSubject:  subject,
		ReplayPolicy:   nats.ReplayInstantPolicy,
		MaxAckPending:  positiveInt(env.GetInt("NATS_LANE_MAX_ACK_PENDING", 20000), 20000),
		DeliverSubject: "_INBOX.BAGEL." + subjectToken(name),
		DeliverGroup:   group,
		Metadata:       map[string]string{managedConsumerMetadata: "true"},
	}
}

type fleetSubscriber struct {
	url   string
	group string
	log   *zap.Logger

	mu        sync.Mutex
	closed    bool
	subs      []Subscriber
	conns     []*nats.Conn
	flowLanes map[string]*sharedFlowLane
	closeCh   chan struct{}

	registrations sync.WaitGroup
}

type subscriptionTarget struct {
	stream string
	topic  string
}

func (s *fleetSubscriber) Subscribe(ctx context.Context, topic string) (<-chan *Message, error) {
	if !s.beginRegistration() {
		return nil, errors.New("bus: subscriber is closed")
	}
	defer s.registrations.Done()

	target, err := targetForTopic(topic)
	if err != nil {
		return nil, err
	}
	sub, conn, messages, err := s.openSubscription(ctx, target)
	if err != nil {
		return nil, err
	}
	if !s.remember(sub, conn) {
		closeSubscription(sub, conn)
		return nil, errors.New("bus: subscriber closed during subscribe")
	}
	s.releaseWhenDone(ctx, sub, conn)
	return messages, nil
}

func targetForTopic(topic string) (subscriptionTarget, error) {
	stream, err := streamForTopic(topic)
	return subscriptionTarget{stream: stream, topic: topic}, err
}

func (s *fleetSubscriber) openSubscription(
	ctx context.Context,
	target subscriptionTarget,
) (Subscriber, *nats.Conn, <-chan *Message, error) {
	sub, conn, err := s.subscriberFor(target)
	if err != nil {
		return nil, nil, nil, err
	}
	messages, err := sub.Subscribe(ctx, target.topic)
	if err != nil {
		closeSubscription(sub, conn)
		return nil, nil, nil, err
	}
	return sub, conn, messages, nil
}

func (s *fleetSubscriber) remember(sub Subscriber, conn *nats.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.subs = append(s.subs, sub)
	if conn != nil {
		s.conns = append(s.conns, conn)
	}
	return true
}

func (s *fleetSubscriber) releaseWhenDone(ctx context.Context, sub Subscriber, conn *nats.Conn) {
	go s.releaseAfterDone(ctx, sub, conn)
}

func (s *fleetSubscriber) releaseAfterDone(ctx context.Context, sub Subscriber, conn *nats.Conn) {
	select {
	case <-ctx.Done():
	case <-s.closeCh:
	}
	s.forget(sub, conn)
	closeSubscription(sub, conn)
}

func (s *fleetSubscriber) beginRegistration() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	if s.closeCh == nil {
		s.closeCh = make(chan struct{})
	}
	s.registrations.Add(1)
	return true
}

func (s *fleetSubscriber) forget(sub Subscriber, conn *nats.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs = slices.DeleteFunc(s.subs, func(x Subscriber) bool { return x == sub })
	if conn != nil {
		s.conns = slices.DeleteFunc(s.conns, func(x *nats.Conn) bool { return x == conn })
	}
}

func closeSubscription(sub Subscriber, conn *nats.Conn) {
	_ = sub.Close()
	if conn != nil {
		conn.Close()
	}
}

func (s *fleetSubscriber) subscriberFor(target subscriptionTarget) (Subscriber, *nats.Conn, error) {
	if s.group == "" {
		return s.broadcastSubscriber(target)
	}
	if mode := s.laneModeFor(target); mode != laneModeExplicit {
		return s.sharedLaneSubscriberFor(target, mode)
	}
	binding := LaneConfig{URL: s.url, Stream: target.stream, Subject: target.topic, Group: s.group}
	maxDeliveries := fleetMaxRedeliveries + 1
	return bindDurable(binding, int(maxDeliveries), newMaxRetryDelay(fleetNakDelay, maxDeliveries), s.log)
}

func laneQualifiesForReceiptAcks(target subscriptionTarget) bool {
	return isHotIngressLane(target.stream, target.topic) || IsCanaryLane(target.stream, target.topic)
}

func (s *fleetSubscriber) laneModeFor(target subscriptionTarget) laneConsumeMode {
	mode := consumeMode()
	declined := mode != laneModeExplicit && !laneQualifiesForReceiptAcks(target)
	if !declined {
		return mode
	}
	s.logger().Info("receipt-level consumption declined outside the hot ingress lanes",
		zap.String("stream", target.stream),
		zap.String("subject", target.topic),
		zap.String("mode", string(mode)))
	return laneModeExplicit
}

type sharedFlowLane struct {
	owner *fleetSubscriber
	key   string
	sub   Subscriber
	refs  int
}

type flowLaneHandle struct {
	lane    *sharedFlowLane
	release sync.Once
}

func (h *flowLaneHandle) Subscribe(ctx context.Context, subject string) (<-chan *Message, error) {
	return h.lane.sub.Subscribe(ctx, subject)
}

func (h *flowLaneHandle) Close() error {
	var err error
	h.release.Do(func() { err = h.lane.owner.releaseFlowLane(h.lane) })
	return err
}

func (s *fleetSubscriber) sharedLaneSubscriberFor(
	target subscriptionTarget,
	mode laneConsumeMode,
) (Subscriber, *nats.Conn, error) {
	lane, err := s.acquireFlowLane(target, mode)
	if err != nil {
		return nil, nil, err
	}
	return &flowLaneHandle{lane: lane}, nil, nil
}

func (s *fleetSubscriber) acquireFlowLane(target subscriptionTarget, mode laneConsumeMode) (*sharedFlowLane, error) {
	key := target.stream + "|" + target.topic
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("bus: subscriber is closed")
	}
	if lane, bound := s.flowLanes[key]; bound {
		lane.refs++
		s.mu.Unlock()
		return lane, nil
	}
	s.mu.Unlock()

	sub, err := newSharedLaneSubscriber(flowLaneConfig{
		url: s.url, stream: target.stream, subject: target.topic, group: s.group, log: s.log,
	}, mode)
	if err != nil {
		return nil, err
	}
	return s.rememberFlowLane(key, sub)
}

func newSharedLaneSubscriber(cfg flowLaneConfig, mode laneConsumeMode) (Subscriber, error) {
	if mode == laneModePull {
		sub, err := newPullLaneSubscriber(cfg)
		if err != nil {
			return nil, err
		}
		return sub, nil
	}
	sub, err := newFlowLaneSubscriber(cfg)
	if err != nil {
		return nil, err
	}
	return sub, nil
}

func (s *fleetSubscriber) rememberFlowLane(key string, sub Subscriber) (*sharedFlowLane, error) {
	lane, surplus, err := s.storeFlowLane(key, sub)
	if surplus {
		_ = sub.Close()
	}
	return lane, err
}

func (s *fleetSubscriber) storeFlowLane(key string, sub Subscriber) (*sharedFlowLane, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, true, errors.New("bus: subscriber closed during subscribe")
	}
	if existing, bound := s.flowLanes[key]; bound {
		existing.refs++
		return existing, true, nil
	}
	if s.flowLanes == nil {
		s.flowLanes = make(map[string]*sharedFlowLane)
	}
	lane := &sharedFlowLane{owner: s, key: key, sub: sub, refs: 1}
	s.flowLanes[key] = lane
	return lane, false, nil
}

func (s *fleetSubscriber) releaseFlowLane(lane *sharedFlowLane) error {
	s.mu.Lock()
	lane.refs--
	if lane.refs > 0 {
		s.mu.Unlock()
		return nil
	}
	delete(s.flowLanes, lane.key)
	s.mu.Unlock()
	return lane.sub.Close()
}

func (s *fleetSubscriber) logger() *zap.Logger {
	if s.log == nil {
		return zap.NewNop()
	}
	return s.log
}

type LaneHealth interface{ Healthy() bool }

func SubscriberHealthy(sub Subscriber) bool {
	reporter, ok := sub.(LaneHealth)
	return !ok || reporter.Healthy()
}

func (s *fleetSubscriber) Healthy() bool {
	s.mu.Lock()
	lanes := make([]Subscriber, 0, len(s.flowLanes))
	for _, lane := range s.flowLanes {
		lanes = append(lanes, lane.sub)
	}
	s.mu.Unlock()

	for _, lane := range lanes {
		if reporter, ok := lane.(LaneHealth); ok && !reporter.Healthy() {
			return false
		}
	}
	return true
}

func (s *fleetSubscriber) broadcastSubscriber(target subscriptionTarget) (Subscriber, *nats.Conn, error) {
	nc, err := nats.Connect(busURL(endpoint(s.url)), busOptions(clientName("broadcast-"+subjectToken(target.topic)))...)
	if err != nil {
		return nil, nil, err
	}
	js, err := nc.JetStream(jsDomainOption()...)
	if err != nil {
		nc.Close()
		return nil, nil, err
	}
	sub := newConcurrentDurableSubscriber(concurrentSubscriberConfig{
		nc: nc, js: js, stream: target.stream, log: s.log,
	})
	return sub, nc, nil
}

func (s *fleetSubscriber) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if s.closeCh != nil {
		close(s.closeCh)
	}
	s.mu.Unlock()

	s.registrations.Wait()

	// Close these outside s.mu: a shared flow lane's Close re-enters releaseFlowLane and would deadlock.
	subs, conns := s.takeResources()

	var errs []error
	for _, sub := range subs {
		if err := sub.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, conn := range conns {
		conn.Close()
	}
	if len(errs) > 0 {
		return fmt.Errorf("fleetSubscriber closed with %d errors, first: %w", len(errs), errs[0])
	}
	return nil
}

func (s *fleetSubscriber) takeResources() ([]Subscriber, []*nats.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	subs, conns := s.subs, s.conns
	s.subs, s.conns = nil, nil
	return subs, conns
}

func newSubscriber(url string, group string, log *zap.Logger) (Subscriber, error) {
	return &fleetSubscriber{
		url:     url,
		group:   group,
		log:     log,
		closeCh: make(chan struct{}),
	}, nil
}

func durableName(group, topic string) string {
	if group == "" {
		return ""
	}
	return group + "_" + subjectToken(topic)
}

func subjectToken(subject string) string {
	return strings.NewReplacer(".", "_", "*", "_", ">", "_").Replace(subject)
}
