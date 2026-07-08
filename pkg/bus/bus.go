package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/internal/utils"

	wmnats "github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

// The bus rides the JetStream cluster from ADR 0003: at-least-once delivery
// with explicit acks, short retention, durable queue groups per service so
// every instance of a service shares one consumer and horizontal scaling
// comes for free.
//
// Streams are never auto-provisioned by the client: watermill's AutoProvision
// names a stream after the (dotted) topic, which JetStream rejects, so it can
// only ever work for single-token topics. The fleet provisions its streams
// explicitly through EnsureStreams (see provision.go) and the publisher and
// subscriber here simply bind to those existing streams by subject.

// NewPublisher connects to NATS and returns a JetStream-backed publisher. The
// target stream must already exist (see EnsureStreams); the publisher binds to
// it by subject.
func NewPublisher(url string, log *zap.Logger) (message.Publisher, error) {

	return wmnats.NewPublisher(wmnats.PublisherConfig{
		URL:         busURL(url),
		NatsOptions: busOptions(""),
		Marshaler:   &wmnats.NATSMarshaler{},
		JetStream: wmnats.JetStreamConfig{
			AutoProvision: false,
			// Dialed at the leaf, so target the authoritative hub JetStream
			// domain rather than the leaf's own.
			ConnectOptions: jsDomainOption(),
		},
	}, newZapAdapter(log))
}

// NewSubscriber connects to NATS and returns a durable JetStream subscriber.
// All instances passing the same group share one durable consumer, so a
// message is processed by exactly one instance and survives restarts.
func NewSubscriber(url string, group string, log *zap.Logger) (message.Subscriber, error) {
	return newSubscriber(url, group, nil, log)
}

// LaneConfig describes one bounded work-queue subscription: the stream it
// binds to, the subject filter, the durable group that shares the consumer,
// and its redelivery budget.
//
// NakDelay paces redelivery after a NACK (rate limits, transient failures),
// applied per message through Watermill's NakWithDelay. It must NOT become a
// consumer-level BackOff: the server forces AckWait down to backoff[0], so a
// short nack delay would also redeliver every message whose handler is merely
// slower than that delay — while the first replica is still working — and fan
// one job out across the whole fleet (duplicate chat sends, duplicate clips).
//
// MaxRedeliveries excludes the first delivery. NATS enforces the total on the
// consumer and the Watermill delay terminates the final failed delivery, so a
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
func NewLaneSubscriber(cfg LaneConfig, log *zap.Logger) (message.Subscriber, error) {
	maxDeliveries := cfg.MaxRedeliveries + 1
	sub, _, err := bindDurable(cfg, int(maxDeliveries), wmnats.NewMaxRetryDelay(cfg.NakDelay, maxDeliveries), log)
	return sub, err
}

// bindDurable connects, provisions the server-owned durable consumer, and
// binds a watermill subscriber to it. Only the binding fields of cfg are read;
// the redelivery pacing arrives resolved as maxDeliveries + nakDelay.
func bindDurable(cfg LaneConfig, maxDeliveries int, nakDelay wmnats.Delay, log *zap.Logger) (message.Subscriber, *nats.Conn, error) {
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

	sub, err := wmnats.NewSubscriberWithNatsConn(nc, wmnats.SubscriberSubscriptionConfig{
		QueueGroupPrefix: cfg.Group,
		SubscribersCount: 1,
		AckWaitTimeout:   30 * time.Second,
		NakDelay:         nakDelay,
		Unmarshaler:      &wmnats.NATSMarshaler{},
		JetStream: wmnats.JetStreamConfig{
			AutoProvision:     false,
			DurablePrefix:     cfg.Group,
			DurableCalculator: durableName,
			ConnectOptions:    jsDomainOption(),
			SubscribeOptions: []nats.SubOpt{
				nats.Bind(cfg.Stream, consumer),
			},
		},
	}, newZapAdapter(log))
	if err != nil {
		nc.Close()
		return nil, nil, err
	}

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
	// already bound to.
	desired.DeliverSubject = info.Config.DeliverSubject
	if _, err := js.UpdateConsumer(stream, desired); err != nil {
		return replaceConsumer(js, stream, desired, err)
	}
	return nil
}

// replaceConsumer falls back to delete + recreate for transitions that are not
// updatable in place (notably clearing a legacy BackOff schedule on older
// servers). The deliver subject and group are deterministic, so replicas
// already bound keep receiving from the recreated consumer, and the lanes are
// perishable work queues with no replay to preserve.
func replaceConsumer(js nats.JetStreamManager, stream string, desired *nats.ConsumerConfig, cause error) error {
	if derr := js.DeleteConsumer(stream, desired.Name); derr != nil && !errors.Is(derr, nats.ErrConsumerNotFound) {
		return fmt.Errorf("bus: update consumer %q: %w (replace failed: %v)", desired.Name, cause, derr)
	}
	if _, aerr := js.AddConsumer(stream, desired); aerr != nil {
		return fmt.Errorf("bus: recreate consumer %q: %w", desired.Name, aerr)
	}
	return nil
}

// laneConsumerConfig deliberately sets no BackOff: the server clamps AckWait to
// backoff[0], and a short first step turns every handler slower than it into a
// premature redelivery to another replica while the first is still working —
// the same job then executes on several pods (duplicate chat sends / clips).
// NACK pacing lives in the subscriber's per-message NakWithDelay instead, which
// leaves AckWait as the sole in-flight redelivery clock.
func laneConsumerConfig(subject, group, name string, maxDeliveries int) *nats.ConsumerConfig {
	return &nats.ConsumerConfig{
		Durable:        name,
		Name:           name,
		Description:    "ItsBagelBot bounded outgress work queue",
		DeliverPolicy:  nats.DeliverAllPolicy,
		AckPolicy:      nats.AckExplicitPolicy,
		AckWait:        30 * time.Second,
		MaxDeliver:     maxDeliveries,
		FilterSubject:  subject,
		ReplayPolicy:   nats.ReplayInstantPolicy,
		MaxAckPending:  1000,
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
	url      string
	group    string
	nakDelay wmnats.Delay
	log      *zap.Logger

	mu    sync.Mutex
	subs  []message.Subscriber
	conns []*nats.Conn
}

func (s *fleetSubscriber) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	stream, err := streamForTopic(topic)
	if err != nil {
		return nil, err
	}

	sub, conn, err := s.subscriberFor(stream, topic)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.subs = append(s.subs, sub)
	if conn != nil {
		s.conns = append(s.conns, conn)
	}
	s.mu.Unlock()

	return sub.Subscribe(ctx, topic)
}

// subscriberFor builds the topic's subscriber: a broadcast ephemeral consumer
// when the group is empty, else a durable queue-group consumer bound to a
// provisioned server-owned durable (so it survives pod disconnects).
func (s *fleetSubscriber) subscriberFor(stream, topic string) (message.Subscriber, *nats.Conn, error) {
	if s.group == "" {
		sub, err := s.broadcastSubscriber()
		return sub, nil, err
	}
	binding := LaneConfig{URL: s.url, Stream: stream, Subject: topic, Group: s.group}
	return bindDurable(binding, 1000, s.nakDelay, s.log)
}

// broadcastSubscriber uses an ephemeral consumer with DeliverNew to avoid
// replay storms: every instance gets every message (cache invalidation).
func (s *fleetSubscriber) broadcastSubscriber() (message.Subscriber, error) {
	return wmnats.NewSubscriber(wmnats.SubscriberConfig{
		URL:              busURL(s.url),
		NatsOptions:      busOptions(s.group),
		SubscribersCount: 1,
		AckWaitTimeout:   30 * time.Second,
		Unmarshaler:      &wmnats.NATSMarshaler{},
		JetStream: wmnats.JetStreamConfig{
			AutoProvision:  false,
			ConnectOptions: jsDomainOption(),
			SubscribeOptions: []nats.SubOpt{
				nats.DeliverNew(),
			},
		},
	}, newZapAdapter(s.log))
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

func newSubscriber(url string, group string, nakDelay wmnats.Delay, log *zap.Logger) (message.Subscriber, error) {
	return &fleetSubscriber{
		url:      url,
		group:    group,
		nakDelay: nakDelay,
		log:      log,
	}, nil
}

// durableName derives the JetStream durable consumer name for a (group, topic)
// pair. watermill's default uses the group prefix alone, which collides the
// moment a service subscribes to more than one subject (the projector folds
// several event subjects). Qualifying the durable with the topic gives each
// subscription its own consumer, surviving restarts deterministically.
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

// tracedMessage builds the wire message with a fresh UUIDv7 id (which
// JetStream also uses for deduplication) and, when ctx carries a New Relic
// transaction, its distributed trace headers in the message metadata so the
// consumer side links to the same trace.
func tracedMessage(ctx context.Context, body []byte) (*message.Message, error) {
	id, err := utils.NewID()
	if err != nil {
		return nil, err
	}

	msg := message.NewMessage(id.String(), body)

	if txn := newrelic.FromContext(ctx); txn != nil {
		headers := http.Header{}
		txn.InsertDistributedTraceHeaders(headers)
		for key := range headers {
			msg.Metadata.Set(key, headers.Get(key))
		}
	}
	return msg, nil
}

// PublishJSON marshals payload and publishes it on subject with a fresh
// UUIDv7 message ID and the caller's distributed trace headers (see
// tracedMessage).
func PublishJSON(ctx context.Context, pub message.Publisher, subject string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return PublishRaw(ctx, pub, subject, body)
}

// PublishRaw is like PublishJSON but takes already-marshaled bytes and skips
// json.Marshal, for hot-path callers that marshal once into a pooled buffer.
func PublishRaw(ctx context.Context, pub message.Publisher, subject string, body []byte) error {
	msg, err := tracedMessage(ctx, body)
	if err != nil {
		return err
	}
	return pub.Publish(subject, msg)
}
