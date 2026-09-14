// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// OpenCoordination uses the same authenticated, TLS-protected hub path as the
// work queues. Coordination must not use the RPC account on the leaf tier.
func OpenCoordination(url string) (jetstream.JetStream, func(), error) {
	nc, err := nats.Connect(busURL(endpoint(url)), busOptions("outgress-coordination")...)
	if err != nil {
		return nil, nil, err
	}
	js, err := jetstream.NewWithDomain(nc, JSDomain())
	if err != nil {
		nc.Close()
		return nil, nil, err
	}
	return js, nc.Close, nil
}

// CoordinationBucket is durable and quorum replicated. TTL bounds transient
// rate/batch state; the pause bucket uses zero TTL so a pause never expires.
func CoordinationBucket(ctx context.Context, js jetstream.JetStream, name string, ttl time.Duration) (jetstream.KeyValue, error) {
	return js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: name, History: 1, TTL: ttl, Replicas: 3,
		Storage: jetstream.FileStorage, MaxBytes: 64 << 20,
	})
}
