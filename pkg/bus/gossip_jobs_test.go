// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus_test

import (
	"testing"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type gossipJobsConfig struct {
	storage       jsapi.StorageType
	retention     jsapi.RetentionPolicy
	maxAge        time.Duration
	duplicates    time.Duration
	replicas      int
	subjects      []string
	ack           jsapi.AckPolicy
	ackWait       time.Duration
	maxDeliver    int
	maxAckPending int
	deliver       jsapi.DeliverPolicy
	filter        string
}

func TestOpenGossipJobsConfig(t *testing.T) {
	ctx := t.Context()
	nc := bustest.JetStream(t)
	_, err := bus.OpenGossipJobs(ctx, nc, 1)
	require.NoError(t, err)
	js, err := jsapi.New(nc)
	require.NoError(t, err)
	stream, err := js.Stream(ctx, "GOSSIP_JOBS")
	require.NoError(t, err)
	consumer, err := js.Consumer(ctx, "GOSSIP_JOBS", "gossip-jobs")
	require.NoError(t, err)

	streamCfg, cfg := stream.CachedInfo().Config, consumer.CachedInfo().Config
	assert.Equal(t, gossipJobsConfig{
		storage: jsapi.MemoryStorage, retention: jsapi.WorkQueuePolicy, maxAge: 10 * time.Minute, duplicates: 10 * time.Minute,
		replicas: 1, subjects: []string{"gossip.jobs.>"},
		ack: jsapi.AckExplicitPolicy, ackWait: 30 * time.Second, maxDeliver: -1, maxAckPending: -1, deliver: jsapi.DeliverAllPolicy,
	}, gossipJobsConfig{
		storage: streamCfg.Storage, retention: streamCfg.Retention, maxAge: streamCfg.MaxAge, duplicates: streamCfg.Duplicates,
		replicas: streamCfg.Replicas, subjects: streamCfg.Subjects,
		ack: cfg.AckPolicy, ackWait: cfg.AckWait, maxDeliver: cfg.MaxDeliver, maxAckPending: cfg.MaxAckPending,
		deliver: cfg.DeliverPolicy, filter: cfg.FilterSubject,
	})
	assert.Equal(t, 3, bus.TTSReplicas)
}

func TestBindGossipJobsRequiresProvisionedResources(t *testing.T) {
	ctx := t.Context()
	nc := bustest.JetStream(t)
	_, err := bus.BindGossipJobs(ctx, nc)
	require.Error(t, err)

	_, err = bus.OpenGossipJobs(ctx, nc, 1)
	require.NoError(t, err)
	_, err = bus.BindGossipJobs(ctx, nc)
	require.NoError(t, err)
}

func TestGossipJobsPublishLandsOnKindAndUserSubject(t *testing.T) {
	ctx := t.Context()
	nc := bustest.JetStream(t)
	jobs, err := bus.OpenGossipJobs(ctx, nc, 1)
	require.NoError(t, err)
	js, err := jsapi.New(nc)
	require.NoError(t, err)

	require.NoError(t, jobs.Publish(ctx, bus.GossipJob{Kind: bus.GossipJobTTS, UserID: "42", EventID: "evt-1", Value: map[string]string{"k": "v"}}))
	consumer, err := js.Consumer(ctx, "GOSSIP_JOBS", "gossip-jobs")
	require.NoError(t, err)
	batch, err := consumer.Fetch(1, jsapi.FetchMaxWait(3*time.Second))
	require.NoError(t, err)
	got := <-batch.Messages()
	require.NotNil(t, got)
	assert.Equal(t, "gossip.jobs.tts.42", got.Subject())
	assert.Equal(t, "evt-1", got.Headers().Get(nats.MsgIdHdr))
	assert.JSONEq(t, `{"k":"v"}`, string(got.Data()))
	require.NoError(t, got.Ack())
}
