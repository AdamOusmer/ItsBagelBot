// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

const (
	TTSJobsStream    = "TTS_JOBS"
	TTSWorkerDurable = "tts-worker"
	TTSReplicas      = 3

	ttsJobsSubject   = "tts.jobs."
	ttsJobTTL        = 10 * time.Minute
	ttsAckWait       = 60 * time.Second
	ttsHandleTimeout = 40 * time.Second
	ttsMaxDeliver    = 3
)

type TTSJobs struct {
	js       jsapi.JetStream
	consumer jsapi.Consumer
}

func ConnectTTSJobs(url string) (*nats.Conn, error) {
	return nats.Connect(busURL(endpoint(url)), busOptions("sesame-tts-jobs")...)
}

func OpenTTSJobs(ctx context.Context, nc *nats.Conn, replicas int) (*TTSJobs, error) {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return nil, err
	}
	spec := StreamSpec{
		Name: TTSJobsStream, Subjects: []string{ttsJobsSubject + "*"}, Storage: nats.FileStorage,
		Retention: nats.WorkQueuePolicy, Replicas: replicas, MaxAge: ttsJobTTL, Duplicates: ttsJobTTL,
	}
	stream, err := createOrGetTTSStream(ctx, js, spec)
	if err != nil {
		return nil, err
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jsapi.ConsumerConfig{
		Durable: TTSWorkerDurable, AckPolicy: jsapi.AckExplicitPolicy,
		AckWait: ttsAckWait, MaxDeliver: ttsMaxDeliver, MaxAckPending: -1,
	})
	if err != nil {
		return nil, err
	}
	return &TTSJobs{js: js, consumer: consumer}, nil
}

func createOrGetTTSStream(ctx context.Context, js jsapi.JetStream, spec StreamSpec) (jsapi.Stream, error) {
	stream, err := js.CreateOrUpdateStream(ctx, streamConfig(spec))
	if errors.Is(err, jsapi.ErrStreamNameAlreadyInUse) {
		return js.Stream(ctx, spec.Name)
	}
	return stream, err
}

type TTSMessage struct {
	BroadcasterID string
	EventID       string
	Value         any
}

func (s *TTSJobs) Publish(ctx context.Context, msg TTSMessage) error {
	return publishTTS(ctx, s.js, ttsPublication{TTSJobsStream, ttsJobsSubject, msg})
}

type ttsPublication struct {
	stream string
	prefix string
	msg    TTSMessage
}

func publishTTS(ctx context.Context, js jsapi.JetStream, pub ttsPublication) error {
	body, err := codec.FastMarshal(pub.msg.Value)
	if err != nil {
		return err
	}
	subject := pub.prefix + pub.msg.BroadcasterID
	segment := startMessagingSegment(ctx, messagingSpan{name: "nats.publish", operation: "publish", destination: subject})
	msg := nats.NewMsg(subject)
	msg.Data = body
	insertQueueTraceHeaders(ctx, msg)
	_, err = js.PublishMsg(ctx, msg, jsapi.WithMsgID(pub.msg.EventID), jsapi.WithExpectStream(pub.stream))
	endMessagingSegment(segment, err)
	return err
}
