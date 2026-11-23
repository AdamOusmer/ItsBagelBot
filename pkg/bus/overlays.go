// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

type OverlayTier string

type OverlayKind string

const (
	OverlayPremium  OverlayTier = "premium"
	OverlayStandard OverlayTier = "standard"

	OverlayKindTTS    OverlayKind = "tts"
	OverlayKindCustom OverlayKind = "custom"

	OverlaysPremiumStream   = "OVERLAYS_PREMIUM"
	OverlaysStandardStream  = "OVERLAYS_STANDARD"
	OverlaysPremiumDurable  = "overlays-premium"
	OverlaysStandardDurable = "overlays-standard"
	OverlaysMediaBucket     = "OVERLAYS_MEDIA"
	OverlayClipTTL          = 5 * time.Minute

	overlaysSubjectRoot = "overlays."
	overlayAckWait      = 30 * time.Second
)

type overlayLane struct {
	tier    OverlayTier
	stream  string
	durable string
}

var overlayLanes = map[OverlayTier]overlayLane{
	OverlayPremium:  {OverlayPremium, OverlaysPremiumStream, OverlaysPremiumDurable},
	OverlayStandard: {OverlayStandard, OverlaysStandardStream, OverlaysStandardDurable},
}

type Overlays struct {
	js     jsapi.JetStream
	bucket jsapi.ObjectStore
}

type OverlayMessage struct {
	Tier    OverlayTier
	Kind    OverlayKind
	UserID  string
	EventID string
	Value   any
}

type OverlayClip struct {
	Key      string
	Metadata map[string]string
	StoredAt time.Time
}

type OverlayClipData struct {
	OverlayClip
	WAV []byte
}

func ConnectOverlays(url string) (*nats.Conn, error) {
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

func OpenOverlays(ctx context.Context, nc *nats.Conn, replicas int) (*Overlays, error) {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return nil, err
	}
	for _, lane := range overlayLanes {
		if err := provisionOverlayLane(ctx, js, lane, replicas); err != nil {
			return nil, err
		}
	}
	bucket, err := js.CreateOrUpdateObjectStore(ctx, jsapi.ObjectStoreConfig{
		Bucket: OverlaysMediaBucket, TTL: OverlayClipTTL, Storage: jsapi.FileStorage, Replicas: replicas,
	})
	if errors.Is(err, jsapi.ErrBucketExists) {
		bucket, err = js.ObjectStore(ctx, OverlaysMediaBucket)
	}
	if err != nil {
		return nil, err
	}
	return &Overlays{js: js, bucket: bucket}, nil
}

func NewOverlayPublisher(nc *nats.Conn) (*Overlays, error) {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return nil, err
	}
	return &Overlays{js: js}, nil
}

func BindOverlays(ctx context.Context, nc *nats.Conn) (*Overlays, error) {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return nil, err
	}
	for _, lane := range overlayLanes {
		if _, err := js.Stream(ctx, lane.stream); err != nil {
			return nil, err
		}
	}
	bucket, err := js.ObjectStore(ctx, OverlaysMediaBucket)
	if err != nil {
		return nil, err
	}
	return &Overlays{js: js, bucket: bucket}, nil
}

func provisionOverlayLane(ctx context.Context, js jsapi.JetStream, lane overlayLane, replicas int) error {
	spec := StreamSpec{
		Name: lane.stream, Subjects: []string{overlaysSubjectRoot + string(lane.tier) + ".>"}, Storage: nats.MemoryStorage,
		Retention: nats.WorkQueuePolicy, Replicas: replicas, MaxAge: OverlayClipTTL, Duplicates: OverlayClipTTL,
	}
	stream, err := createOrGetStream(ctx, js, spec)
	if err != nil {
		return err
	}
	_, err = stream.CreateOrUpdateConsumer(ctx, jsapi.ConsumerConfig{
		Durable: lane.durable, AckPolicy: jsapi.AckExplicitPolicy, AckWait: overlayAckWait,
		MaxDeliver: -1, MaxAckPending: -1, DeliverPolicy: jsapi.DeliverAllPolicy,
	})
	return err
}

func (o *Overlays) Publish(ctx context.Context, msg OverlayMessage) error {
	lane, ok := overlayLanes[msg.Tier]
	if !ok {
		return fmt.Errorf("bus: unknown overlay tier %q", msg.Tier)
	}
	return publishQueued(ctx, o.js, queuedMessage{
		stream:  lane.stream,
		subject: overlaysSubjectRoot + string(msg.Tier) + "." + string(msg.Kind) + "." + msg.UserID,
		eventID: msg.EventID,
		value:   msg.Value,
	})
}

func (o *Overlays) GetClip(ctx context.Context, ref OverlayClip) (OverlayClip, error) {
	info, err := o.bucket.GetInfo(ctx, ref.Key)
	if errors.Is(err, jsapi.ErrObjectNotFound) {
		return OverlayClip{}, nil
	}
	return storedOverlayClip(info, err)
}

func (o *Overlays) PutClip(ctx context.Context, data OverlayClipData) (OverlayClip, error) {
	segment := startMessagingSegment(ctx, messagingSpan{name: "nats.object.put", operation: "put", destination: OverlaysMediaBucket})
	info, err := o.bucket.Put(ctx, jsapi.ObjectMeta{Name: data.Key, Metadata: data.Metadata}, bytes.NewReader(data.WAV))
	endMessagingSegment(segment, err)
	return storedOverlayClip(info, err)
}

func storedOverlayClip(info *jsapi.ObjectInfo, err error) (OverlayClip, error) {
	if err != nil {
		return OverlayClip{}, err
	}
	return OverlayClip{Key: info.Name, Metadata: info.Metadata, StoredAt: info.ModTime}, nil
}
