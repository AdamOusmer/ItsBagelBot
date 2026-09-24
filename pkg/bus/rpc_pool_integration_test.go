// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
)

func TestQueueSubscribeRPCConcurrentIntegration(t *testing.T) {
	nc := dialIntegrationBroker(t)
	subject := "bagel.rpc.pooltest." + nuid.Next()
	probe := newOverlapProbe(4)

	registration := RPCSubscription{
		Subject:    subject,
		QueueGroup: "pooltest",
		Policy:     RPCPoolPolicy{MaxWorkers: 2, QueueDepth: 2},
	}
	pool, err := QueueSubscribeRPCConcurrent(nc, registration, func(msg *nats.Msg) {
		probe.handle(msg)
		_ = msg.Respond([]byte(`{"ok":true}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}

	subjects := rpcSubscriptionSubjects(rpcSubject(subject))
	if len(subjects) != 2 {
		t.Fatalf("registered subjects = %v, want the generic and node-local pair", subjects)
	}

	replies, requests := requestEach(nc, subjects, 4)

	probe.awaitArrivals(t, 2)
	close(probe.release)

	waitGroupWithin(t, requests, "the four requests to be answered")
	requireNoReplyErrors(t, replies)

	if got := int(probe.peak.Load()); got != 2 {
		t.Fatalf("peak concurrent handlers across both subjects = %d, want 2", got)
	}
	requireDrained(t, drainAsync(pool))
}

func dialIntegrationBroker(t *testing.T) *nats.Conn {
	t.Helper()
	url := os.Getenv("NATS_INTEGRATION_URL")
	if url == "" {
		t.Skip("NATS_INTEGRATION_URL is not set")
	}
	t.Setenv("NODE_NAME", "pooltest")

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return nc
}

func requestEach(nc *nats.Conn, subjects []string, n int) (<-chan error, *sync.WaitGroup) {
	replies := make(chan error, n)
	var requests sync.WaitGroup
	for i := range n {
		requests.Add(1)
		go func(routed string) {
			defer requests.Done()
			ctx, cancel := context.WithTimeout(context.Background(), rpcPoolTestTimeout)
			defer cancel()
			_, err := nc.RequestWithContext(ctx, routed, []byte(`{}`))
			replies <- err
		}(subjects[i%len(subjects)])
	}
	return replies, &requests
}

func requireNoReplyErrors(t *testing.T, replies <-chan error) {
	t.Helper()
	for len(replies) > 0 {
		if err := <-replies; err != nil {
			t.Fatalf("request failed: %v", err)
		}
	}
}
