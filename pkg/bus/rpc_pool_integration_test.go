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

// TestQueueSubscribeRPCConcurrentIntegration exercises the whole registered path
// — nats.go delivery goroutine, pool hand-off, worker, respond — against the
// repository's opt-in broker. The unit tests drive the pool directly; this one
// proves the subscription is actually wired to it. The ordinary suite skips it
// so CI does not need an external server.
func TestQueueSubscribeRPCConcurrentIntegration(t *testing.T) {
	nc := dialIntegrationBroker(t)
	subject := "bagel.rpc.pooltest." + nuid.Next()
	probe := newOverlapProbe(4)

	// MaxWorkers 2 is the whole assertion: four requests split across the two
	// registered subjects must peak at TWO concurrent handlers. A peak of four
	// would mean the generic and node-local subscriptions had been given a fleet
	// each, doubling the budget the ceiling exists to bound.
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

	// Two handlers must be running at once; the old inline callback would have
	// started exactly one per subscription and finished neither.
	probe.awaitArrivals(t, 2)
	close(probe.release)

	waitGroupWithin(t, requests, "the four requests to be answered")
	requireNoReplyErrors(t, replies)

	if got := int(probe.peak.Load()); got != 2 {
		t.Fatalf("peak concurrent handlers across both subjects = %d, want 2", got)
	}
	requireDrained(t, drainAsync(pool))
}

// dialIntegrationBroker connects to the broker the integration tests need, or
// skips when none is configured.
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

// requestEach fires n requests spread round-robin across subjects, so both the
// generic and the node-local subscription receive traffic.
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

// requireNoReplyErrors drains the collected request outcomes and fails on the
// first error.
func requireNoReplyErrors(t *testing.T, replies <-chan error) {
	t.Helper()
	for len(replies) > 0 {
		if err := <-replies; err != nil {
			t.Fatalf("request failed: %v", err)
		}
	}
}
