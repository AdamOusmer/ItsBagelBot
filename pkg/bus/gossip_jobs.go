// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

const (
	GossipJobsStream  = "GOSSIP_JOBS"
	GossipJobsDurable = "gossip-jobs"
	TTSReplicas       = 3

	gossipJobsSubjectRoot = "gossip.jobs."
	gossipJobTTL          = 10 * time.Minute
	gossipJobAckWait      = 30 * time.Second
)

type GossipJobKind string

const GossipJobTTS GossipJobKind = "tts"

type GossipJob struct {
	Kind    GossipJobKind
	UserID  string
	EventID string
	Value   any
}

type GossipJobs struct {
	js       jsapi.JetStream
	consumer jsapi.Consumer
}

func ConnectGossipJobs(url string) (*nats.Conn, error) {
	identity := connectionIdentity{
		name:     "gossip-jobs",
		jwt:      env.Get("GOSSIP_JOBS_BUS_JWT", ""),
		nkeySeed: env.Get("GOSSIP_JOBS_BUS_NKEY_SEED", ""),
	}
	if identity.jwt == "" || identity.nkeySeed == "" {
		return nil, errors.New("bus: missing Gossip jobs credentials")
	}
	return nats.Connect(busURL(endpoint(url)), baseOptions(identity)...)
}

func OpenGossipJobs(ctx context.Context, nc *nats.Conn, replicas int) (*GossipJobs, error) {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return nil, err
	}
	stream, err := createOrGetStream(ctx, js, StreamSpec{
		Name: GossipJobsStream, Subjects: []string{gossipJobsSubjectRoot + ">"}, Storage: nats.MemoryStorage,
		Retention: nats.WorkQueuePolicy, Replicas: replicas, MaxAge: gossipJobTTL, Duplicates: gossipJobTTL,
	})
	if err != nil {
		return nil, err
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jsapi.ConsumerConfig{
		Durable: GossipJobsDurable, AckPolicy: jsapi.AckExplicitPolicy, AckWait: gossipJobAckWait,
		MaxDeliver: -1, MaxAckPending: -1, DeliverPolicy: jsapi.DeliverAllPolicy,
	})
	if err != nil {
		return nil, err
	}
	return &GossipJobs{js: js, consumer: consumer}, nil
}

func BindGossipJobs(ctx context.Context, nc *nats.Conn) (*GossipJobs, error) {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return nil, err
	}
	stream, err := js.Stream(ctx, GossipJobsStream)
	if err != nil {
		return nil, err
	}
	consumer, err := stream.Consumer(ctx, GossipJobsDurable)
	if err != nil {
		return nil, err
	}
	return &GossipJobs{js: js, consumer: consumer}, nil
}

func (j *GossipJobs) Publish(ctx context.Context, job GossipJob) error {
	return publishQueued(ctx, j.js, queuedMessage{
		stream:  GossipJobsStream,
		subject: gossipJobsSubjectRoot + string(job.Kind) + "." + job.UserID,
		eventID: job.EventID,
		value:   job.Value,
	})
}

func parseGossipJobSubject(subject string) (GossipJobKind, string, error) {
	tokens := strings.Split(subject, ".")
	if !validGossipJobTokens(tokens) {
		return "", "", fmt.Errorf("bus: invalid gossip job subject %q", subject)
	}
	return GossipJobKind(tokens[2]), tokens[3], nil
}

func validGossipJobTokens(tokens []string) bool {
	if len(tokens) != 4 || strings.Join(tokens[:2], ".")+"." != gossipJobsSubjectRoot {
		return false
	}
	return !slices.Contains(tokens[2:], "")
}

type queuedMessage struct {
	stream  string
	subject string
	eventID string
	value   any
}

func createOrGetStream(ctx context.Context, js jsapi.JetStream, spec StreamSpec) (jsapi.Stream, error) {
	stream, err := js.CreateOrUpdateStream(ctx, streamConfig(spec))
	if errors.Is(err, jsapi.ErrStreamNameAlreadyInUse) {
		return js.Stream(ctx, spec.Name)
	}
	return stream, err
}

func publishQueued(ctx context.Context, js jsapi.JetStream, queued queuedMessage) error {
	body, err := codec.FastMarshal(queued.value)
	if err != nil {
		return err
	}
	segment := startMessagingSegment(ctx, messagingSpan{name: "nats.publish", operation: "publish", destination: queued.subject})
	msg := nats.NewMsg(queued.subject)
	msg.Data = body
	insertQueueTraceHeaders(ctx, msg)
	_, err = js.PublishMsg(ctx, msg, jsapi.WithMsgID(queued.eventID), jsapi.WithExpectStream(queued.stream))
	endMessagingSegment(segment, err)
	return err
}
