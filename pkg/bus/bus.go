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
)

// The bus rides the JetStream cluster from ADR 0003: at-least-once delivery
// with explicit acks, short retention, durable queue groups per service so
// every instance of a service shares one consumer and horizontal scaling
// comes for free.
//
// Streams are never auto-provisioned by the client. The fleet provisions its
// streams explicitly through EnsureStreams (see provision.go), and the native
// publisher and subscriber simply bind to those existing streams by subject.

// NewSubscriber connects to NATS and returns a durable JetStream subscriber.
// All instances passing the same group share one durable consumer, so a
// message is processed by exactly one instance and survives restarts.
func NewSubscriber(url string, group string, log *zap.Logger) (Subscriber, error) {
	return newSubscriber(url, group, log)
}

// Redelivery budget for the durable consumers behind NewSubscriber (the
// ingress lanes, the data.> event folds, the stream lane). Retries are paced
// per message by NakWithDelay — a plain NACK redelivers immediately, so a
// message whose handler fails deterministically would otherwise grind through
// its whole budget in seconds, fleet-wide, at full pipeline cost. The pacing
// also gives a transient dependency blip (~15s) time to clear; after the
// budget the message is TERMed and ages out of the stream.
const (
	fleetMaxRedeliveries uint64 = 5
	fleetNakDelay               = 3 * time.Second
)

// LaneConfig describes one bounded work-queue subscription: the stream it
// binds to, the subject filter, the durable group that shares the consumer,
// and its redelivery budget.
//
// NakDelay paces redelivery after a NACK (rate limits, transient failures),
// applied per message through nats.go's NakWithDelay. It must NOT become a
// consumer-level BackOff: the server forces AckWait down to backoff[0], so a
// short nack delay would also redeliver every message whose handler is merely
// slower than that delay — while the first replica is still working — and fan
// one job out across the whole fleet (duplicate chat sends, duplicate clips).
//
// MaxRedeliveries excludes the first delivery. NATS enforces the total on the
// consumer and the native delay policy terminates the final failed delivery, so a
// failed command cannot come back after its budget is exhausted.
type LaneConfig struct {
	URL             string
	Stream          string
	Subject         string
	Group           string
	NakDelay        time.Duration
	MaxRedeliveries uint64
}

// NewLaneSubscriber binds to a server-owned durable work-queue consumer. The
// explicit Bind is important: a consumer created implicitly by nats.go is
// deleted when the creating pod unsubscribes, which used to erase the shared
// ACK floor during every rolling update and replay the retained stream.
func NewLaneSubscriber(cfg LaneConfig, log *zap.Logger) (Subscriber, error) {
	maxDeliveries := cfg.MaxRedeliveries + 1
	sub, _, err := bindDurable(cfg, int(maxDeliveries), newMaxRetryDelay(cfg.NakDelay, maxDeliveries), log)
	return sub, err
}

// bindDurable connects, provisions the server-owned durable consumer, and
// binds a native nats.go subscriber to it. Only the binding fields of cfg are read;
// the redelivery pacing arrives resolved as maxDeliveries + nakDelay.
func bindDurable(cfg LaneConfig, maxDeliveries int, nakDelay redeliveryDelay, log *zap.Logger) (Subscriber, *nats.Conn, error) {
	consumer := durableName(cfg.Group, cfg.Subject)

	nc, err := nats.Connect(busURL(cfg.URL), busOptions(cfg.Group)...)
	if err != nil {
		return nil, nil, err
	}

	js, err := nc.JetStream(jsDomainOption()...)
	if err != nil {
		nc.Close()
		return nil, nil, err
	}
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

	// Update mutable parameters, keeping the deliver subject replicas are
	// already bound to and the creation-time delivery position (a consumer
	// recreated at an ack-floor start sequence must not be forced back to
	// DeliverAll, which is not updatable and would trip a replace every boot).
	desired.DeliverSubject = info.Config.DeliverSubject
	desired.DeliverPolicy = info.Config.DeliverPolicy
	desired.OptStartSeq = info.Config.OptStartSeq
	if _, err := js.UpdateConsumer(stream, desired); err != nil {
		carryAckFloor(desired, info)
		return replaceConsumer(js, stream, desired, err)
	}
	return nil
}

// replaceConsumer falls back to delete + recreate for transitions that are not
// updatable in place (notably clearing a legacy BackOff schedule on older
// servers). The deliver subject and group are deterministic, so replicas
// already bound keep receiving from the recreated consumer. The caller has
// already rewritten desired's delivery position to the predecessor's ack
// floor (see carryAckFloor), so the recreation never replays retained
// messages the group has handled.
func replaceConsumer(js nats.JetStreamManager, stream string, desired *nats.ConsumerConfig, cause error) error {
	if derr := js.DeleteConsumer(stream, desired.Name); derr != nil && !errors.Is(derr, nats.ErrConsumerNotFound) {
		return fmt.Errorf("bus: update consumer %q: %w (replace failed: %v)", desired.Name, cause, derr)
	}
	if _, aerr := js.AddConsumer(stream, desired); aerr != nil {
		return fmt.Errorf("bus: recreate consumer %q: %w", desired.Name, aerr)
	}
	return nil
}

// carryAckFloor rewrites desired's delivery position to resume just past what
// the predecessor consumer had fully acknowledged. A floor of zero means
// nothing was ever acked, where starting from the beginning is correct.
func carryAckFloor(desired *nats.ConsumerConfig, info *nats.ConsumerInfo) {
	if info == nil || info.AckFloor.Stream == 0 {
		return
	}
	desired.DeliverPolicy = nats.DeliverByStartSequencePolicy
	desired.OptStartSeq = info.AckFloor.Stream + 1
}

// laneConsumerConfig deliberately sets no BackOff: the server clamps AckWait to
// backoff[0], and a short first step turns every handler slower than it into a
// premature redelivery to another replica while the first is still working —
// the same job then executes on several pods (duplicate chat sends / clips).
// NACK pacing lives in the subscriber's per-message NakWithDelay instead, which
// leaves AckWait as the sole in-flight redelivery clock.
func laneConsumerConfig(subject, group, name string, maxDeliveries int) *nats.ConsumerConfig {
	return &nats.ConsumerConfig{
		Durable:       name,
		Name:          name,
		Description:   "ItsBagelBot bounded work-queue lane consumer",
		DeliverPolicy: nats.DeliverAllPolicy,
		AckPolicy:     nats.AckExplicitPolicy,
		// Handlers send InProgress once per second, so a short AckWait bounds the
		// replay gap after a disconnect without duplicating genuinely slow work.
		// It also stays inside the perishable outgress stream's 5s dedup window.
		AckWait:       4 * time.Second,
		MaxDeliver:    maxDeliveries,
		FilterSubject: subject,
		ReplayPolicy:  nats.ReplayInstantPolicy,
		// Ceiling on unacked messages the server will push to this queue group at
		// once. It must exceed the group's aggregate in-flight concurrency
		// (routines × replicas × per-message latency × target rate) or the server
		// stops delivering and the pipeline stalls below that rate. At ~15 ms/event
		// a 100k/s target needs ~1,500 in flight; 20,000 leaves headroom for
		// latency spikes and burst scale-up without re-tuning per deploy.
		MaxAckPending:  20000,
		DeliverSubject: "_INBOX.BAGEL." + subjectToken(name),
		DeliverGroup:   group,
		Metadata:       map[string]string{managedConsumerMetadata: "true"},
	}
}

// matchSubject reports whether subject falls under filter ('>' matches any
// suffix).
func matchSubject(subject, filter string) bool {
	if strings.HasSuffix(filter, ">") {
		return strings.HasPrefix(subject, strings.TrimSuffix(filter, ">"))
	}
	return subject == filter
}

func matchesAnySubject(topic string, filters []string) bool {
	for _, filter := range filters {
		if matchSubject(topic, filter) {
			return true
		}
	}
	return false
}

func streamForTopic(topic string) (string, error) {
	specs := make([]StreamSpec, 0, len(DataStreams)+2)
	specs = append(specs, DataStreams...)
	specs = append(specs, OutgressStream, OutgressSystemStream)

	for _, spec := range specs {
		if matchesAnySubject(topic, spec.Subjects) {
			return spec.Name, nil
		}
	}
	return "", fmt.Errorf("bus: no stream matches subject %q", topic)
}

type fleetSubscriber struct {
	url   string
	group string
	log   *zap.Logger

	mu    sync.Mutex
	subs  []Subscriber
	conns []*nats.Conn
}

type subscriptionTarget struct {
	stream string
	topic  string
}

func (s *fleetSubscriber) Subscribe(ctx context.Context, topic string) (<-chan *Message, error) {
	stream, err := streamForTopic(topic)
	if err != nil {
		return nil, err
	}

	target := subscriptionTarget{stream: stream, topic: topic}
	sub, conn, err := s.subscriberFor(target)
	if err != nil {
		return nil, err
	}

	messages, err := sub.Subscribe(ctx, topic)
	if err != nil {
		closeSubscription(sub, conn)
		return nil, err
	}

	s.mu.Lock()
	s.subs = append(s.subs, sub)
	if conn != nil {
		s.conns = append(s.conns, conn)
	}
	s.mu.Unlock()

	// A subscription's resources live exactly as long as its ctx. The weighted
	// consumer adds and retires units with load, each unit subscribing under
	// its own ctx; without this a retired unit's NATS connection would sit open
	// until process shutdown, one more per scale cycle.
	go func() {
		<-ctx.Done()
		s.forget(sub, conn)
		closeSubscription(sub, conn)
	}()

	return messages, nil
}

// forget drops a subscription's entries from the shutdown bookkeeping once its
// own ctx has released them, so Close does not double-close.
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

// subscriberFor builds the topic's subscriber: a broadcast ephemeral consumer
// when the group is empty, else a durable queue-group consumer bound to a
// provisioned server-owned durable (so it survives pod disconnects), with the
// shared fleet redelivery budget (see fleetMaxRedeliveries).
func (s *fleetSubscriber) subscriberFor(target subscriptionTarget) (Subscriber, *nats.Conn, error) {
	if s.group == "" {
		return s.broadcastSubscriber(target)
	}
	binding := LaneConfig{URL: s.url, Stream: target.stream, Subject: target.topic, Group: s.group}
	maxDeliveries := fleetMaxRedeliveries + 1
	return bindDurable(binding, int(maxDeliveries), newMaxRetryDelay(fleetNakDelay, maxDeliveries), s.log)
}

// broadcastSubscriber uses an ephemeral consumer with DeliverNew to avoid
// replay storms: every instance gets every message (cache invalidation). The
// explicit stream binding avoids an account-wide stream-name lookup.
func (s *fleetSubscriber) broadcastSubscriber(target subscriptionTarget) (Subscriber, *nats.Conn, error) {
	nc, err := nats.Connect(busURL(s.url), busOptions("broadcast-"+subjectToken(target.topic))...)
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
	defer s.mu.Unlock()

	var errs []error
	for _, sub := range s.subs {
		if err := sub.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, conn := range s.conns {
		conn.Close()
	}
	if len(errs) > 0 {
		return fmt.Errorf("fleetSubscriber closed with %d errors, first: %w", len(errs), errs[0])
	}
	return nil
}

func newSubscriber(url string, group string, log *zap.Logger) (Subscriber, error) {
	return &fleetSubscriber{
		url:   url,
		group: group,
		log:   log,
	}, nil
}

// durableName derives the JetStream durable consumer name for a (group, topic)
// pair. Qualifying the durable by subject avoids collisions when one service
// subscribes to more than one subject (the projector folds several event
// subjects) and gives every binding a deterministic restart-safe consumer.
//
// An empty group means a broadcast subscriber: it keeps an empty durable so
// every instance gets an ephemeral consumer and therefore every message
// (used for cache invalidation), rather than sharing one durable.
func durableName(group, topic string) string {
	if group == "" {
		return ""
	}
	return group + "_" + subjectToken(topic)
}

// subjectToken turns a dotted subject into a token usable in a JetStream
// consumer name, which may not contain dots or wildcards.
func subjectToken(subject string) string {
	return strings.NewReplacer(".", "_", "*", "_", ">", "_").Replace(subject)
}
