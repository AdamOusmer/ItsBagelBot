// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"bytes"
	"context"
	"errors"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

const (
	TTSJobsStream    = "TTS_JOBS"
	TTSClipBucket    = "TTS_CLIPS"
	TTSOverlayStream = "OVERLAY_TTS"
	TTSWorkerDurable = "tts-worker"
	TTSClipTTL       = 5 * time.Minute
	TTSReplicas      = 3

	ttsJobsSubject    = "tts.jobs."
	ttsOverlaySubject = "overlay.tts."
	ttsJobTTL         = 10 * time.Minute
	ttsAckWait        = 60 * time.Second
	ttsHandleTimeout  = 40 * time.Second
	ttsMaxDeliver     = 3
)

type TTSJobs struct {
	js       jsapi.JetStream
	consumer jsapi.Consumer
}

type TTSOverlay struct {
	js     jsapi.JetStream
	bucket jsapi.ObjectStore
}

func ConnectTTSJobs(url string) (*nats.Conn, error) {
	return nats.Connect(busURL(endpoint(url)), busOptions("sesame-tts-jobs")...)
}

func ConnectTTSOverlay(url string) (*nats.Conn, error) {
	identity := connectionIdentity{
		name:     "sesame-tts-overlay",
		jwt:      env.Get("WORKER_TTS_BUS_JWT", ""),
		nkeySeed: env.Get("WORKER_TTS_BUS_NKEY_SEED", ""),
	}
	if identity.jwt == "" || identity.nkeySeed == "" {
		return nil, errors.New("bus: missing Sesame overlay writer credentials")
	}
	return nats.Connect(busURL(endpoint(url)), baseOptions(identity)...)
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
	stream, err := js.CreateOrUpdateStream(ctx, streamConfig(spec))
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

func OpenTTSOverlay(ctx context.Context, nc *nats.Conn, replicas int) (*TTSOverlay, error) {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return nil, err
	}
	spec := StreamSpec{
		Name: TTSOverlayStream, Subjects: []string{ttsOverlaySubject + "*"}, Storage: nats.FileStorage,
		Replicas: replicas, MaxAge: TTSClipTTL, Duplicates: TTSClipTTL,
	}
	if _, err := js.CreateOrUpdateStream(ctx, streamConfig(spec)); err != nil {
		return nil, err
	}
	bucket, err := js.CreateOrUpdateObjectStore(ctx, jsapi.ObjectStoreConfig{
		Bucket: TTSClipBucket, TTL: TTSClipTTL, Storage: jsapi.FileStorage, Replicas: replicas,
	})
	if err != nil {
		return nil, err
	}
	return &TTSOverlay{js: js, bucket: bucket}, nil
}

type TTSMessage struct {
	BroadcasterID string
	EventID       string
	Value         any
}

func (s *TTSJobs) Publish(ctx context.Context, msg TTSMessage) error {
	return publishTTS(ctx, s.js, ttsPublication{TTSJobsStream, ttsJobsSubject, msg})
}

func (s *TTSOverlay) Publish(ctx context.Context, msg TTSMessage) error {
	return publishTTS(ctx, s.js, ttsPublication{TTSOverlayStream, ttsOverlaySubject, msg})
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

type TTSClip struct {
	Key      string
	Metadata map[string]string
	StoredAt time.Time
}

type TTSClipData struct {
	TTSClip
	WAV []byte
}

func (s *TTSOverlay) GetClip(ctx context.Context, ref TTSClip) (TTSClip, error) {
	info, err := s.bucket.GetInfo(ctx, ref.Key)
	if errors.Is(err, jsapi.ErrObjectNotFound) {
		return TTSClip{}, nil
	}
	return storedTTSClip(info, err)
}

func (s *TTSOverlay) PutClip(ctx context.Context, data TTSClipData) (TTSClip, error) {
	segment := startMessagingSegment(ctx, messagingSpan{name: "nats.object.put", operation: "put", destination: TTSClipBucket})
	info, err := s.bucket.Put(ctx, jsapi.ObjectMeta{Name: data.Key, Metadata: data.Metadata}, bytes.NewReader(data.WAV))
	endMessagingSegment(segment, err)
	return storedTTSClip(info, err)
}

func storedTTSClip(info *jsapi.ObjectInfo, err error) (TTSClip, error) {
	if err != nil {
		return TTSClip{}, err
	}
	return TTSClip{Key: info.Name, Metadata: info.Metadata, StoredAt: info.ModTime}, nil
}
