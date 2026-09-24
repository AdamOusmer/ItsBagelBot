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

func TestPasswordModeSmoke(t *testing.T) {
	nodes := buildMigrateTopology(t, 3)
	cluster := startMigrateCluster(t, nodes, func(n migrateNode) string {
		return passwordModeConf(n, nodes)
	})
	defer cluster.shutdown()

	nc, err := nats.Connect(cluster.clientURL(), nats.UserInfo("bus", migratePassword), nats.Timeout(5*time.Second))
	if err != nil {
		t.Fatalf("connect as bus password user: %v", err)
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
		t.Fatalf("create stream through password mode BUS account: %v", err)
	}
}
