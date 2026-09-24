// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// TestOperatorModeSmoke is a fast sanity check that a 3-node operator-mode
// cluster with a JWT-issued BUS account actually serves JetStream through
// the hub domain, before the full rehearsal invests in fixtures.
func TestOperatorModeSmoke(t *testing.T) {
	nodes := buildMigrateTopology(t, 3)
	op := newOperatorFixture(t)
	cluster := startMigrateCluster(t, nodes, func(n migrateNode) string {
		return operatorModeConf(n, nodes, op)
	})
	defer cluster.shutdown()

	busJWT, busSeed := newRoleUser(t, op.ring, "BUS", "bus")
	nc, err := nats.Connect(cluster.clientURL(), nats.UserJWTAndSeed(busJWT, busSeed), nats.Timeout(5*time.Second))
	if err != nil {
		t.Fatalf("connect as BUS role user: %v", err)
	}
	defer nc.Close()
	waitForJetStreamReady(t, nc, "hub")

	js, err := jetstream.NewWithDomain(nc, "hub")
	if err != nil {
		t.Fatalf("jetstream client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "SMOKE", Subjects: []string{"smoke.>"}}); err != nil {
		t.Fatalf("create stream through operator mode BUS account: %v", err)
	}
}
