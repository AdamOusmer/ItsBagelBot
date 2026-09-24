// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"bytes"
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/natsmigrate"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	limitsStreamName    = "REHEARSAL_LIMITS"
	workQueueStreamName = "REHEARSAL_WORKQUEUE"
)

func streamNamesUnderTest() []string {
	return []string{limitsStreamName, workQueueStreamName, "KV_" + kvBucket}
}

// TestRehearsalCutover rehearses the operator-mode cutover end to end: seed
// a password-mode cluster shaped like production, snapshot every stream,
// cut the same store dirs over to operator mode, and restore.
func TestRehearsalCutover(t *testing.T) {
	nodes := buildMigrateTopology(t, 3)
	op := newOperatorFixture(t)
	busJWT, busSeed := newRoleUser(t, op.ring, "BUS", "bus")

	before, snapshots := rehearsePasswordPhase(t, nodes, busJWT, busSeed)
	t.Run("OperatorModeDoesNotAdoptPasswordModeStreams", func(t *testing.T) {
		assertOperatorModeStartsEmpty(t, nodes, op)
	})
	rehearseOperatorPhase(t, nodes, op, busJWT, busSeed, before, snapshots)
}

// rehearsePasswordPhase covers deliverables (a)-(c) and (f)'s password-mode
// half: start the cluster, seed fixtures, prove dual credentials connect,
// and snapshot every stream.
func rehearsePasswordPhase(t *testing.T, nodes []migrateNode, busJWT, busSeed string) ([]streamFixture, map[string]*bytes.Buffer) {
	t.Helper()
	cluster := startMigrateCluster(t, nodes, func(n migrateNode) string { return passwordModeConf(n, nodes) })

	assertDualCredentialConnect(t, cluster, busJWT, busSeed)

	nc := connectPasswordBUS(t, cluster)
	defer nc.Close()
	waitForJetStreamReady(t, nc, "hub")

	js, err := jetstream.NewWithDomain(nc, "hub")
	if err != nil {
		t.Fatalf("jetstream client: %v", err)
	}

	fixtures := []streamFixture{
		seedLimitsStream(t, nc, js, limitsStreamName),
		seedWorkQueueStream(t, nc, js, workQueueStreamName),
	}
	seedKVBucket(t, js)

	snapshots := snapshotStreams(t, nc, streamNamesUnderTest())
	nc.Close()
	cluster.shutdown()
	return fixtures, snapshots
}

func connectPasswordBUS(t *testing.T, cluster *migrateCluster) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(cluster.clientURL(), nats.UserInfo("bus", migratePassword), nats.Timeout(5*time.Second))
	if err != nil {
		t.Fatalf("connect as bus password user: %v", err)
	}
	return nc
}

func snapshotStreams(t *testing.T, nc *nats.Conn, names []string) map[string]*bytes.Buffer {
	t.Helper()
	snapshots := make(map[string]*bytes.Buffer, len(names))
	for _, name := range names {
		var buf bytes.Buffer
		stats, err := natsmigrate.Snapshot(nc, name, &buf, natsmigrate.SnapshotOptions{Domain: "hub"})
		if err != nil {
			t.Fatalf("snapshot %s: %v", name, err)
		}
		t.Logf("snapshot %s: %d bytes", name, stats.Bytes)
		snapshots[name] = &buf
	}
	return snapshots
}

// assertOperatorModeStartsEmpty answers deliverable (d): after the same
// store dirs come back in operator mode, the BUS account's new identity
// (its public key) does not see the password-mode account's streams.
func assertOperatorModeStartsEmpty(t *testing.T, nodes []migrateNode, op *operatorFixture) {
	t.Helper()
	cluster := startMigrateCluster(t, nodes, func(n migrateNode) string { return operatorModeConf(n, nodes, op) })
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
	names := listStreamNames(t, js)
	if len(names) != 0 {
		t.Fatalf("operator-mode BUS account adopted password-mode streams: %v", names)
	}
}

// rehearseOperatorPhase covers deliverables (e) and (f)'s operator-mode
// half: restore every snapshot into a fresh operator-mode cluster on the
// same store dirs and assert it matches the password-mode fixtures, then
// prove the same dual-credential client also connects here.
func rehearseOperatorPhase(t *testing.T, nodes []migrateNode, op *operatorFixture, busJWT, busSeed string, before []streamFixture, snapshots map[string]*bytes.Buffer) {
	t.Helper()
	cluster := startMigrateCluster(t, nodes, func(n migrateNode) string { return operatorModeConf(n, nodes, op) })
	defer cluster.shutdown()

	assertDualCredentialConnect(t, cluster, busJWT, busSeed)

	nc, err := nats.Connect(cluster.clientURL(), nats.UserJWTAndSeed(busJWT, busSeed), nats.Timeout(5*time.Second))
	if err != nil {
		t.Fatalf("connect as BUS role user: %v", err)
	}
	defer nc.Close()
	waitForJetStreamReady(t, nc, "hub")

	restoreStreams(t, nc, streamNamesUnderTest(), snapshots)

	js, err := jetstream.NewWithDomain(nc, "hub")
	if err != nil {
		t.Fatalf("jetstream client: %v", err)
	}
	for _, want := range before {
		assertStreamRestored(t, nc, js, want)
	}
	assertKVRestored(t, js)
}

func restoreStreams(t *testing.T, nc *nats.Conn, names []string, snapshots map[string]*bytes.Buffer) {
	t.Helper()
	for _, name := range names {
		stats, err := natsmigrate.Restore(nc, snapshots[name], natsmigrate.RestoreOptions{Domain: "hub"})
		if err != nil {
			t.Fatalf("restore %s: %v", name, err)
		}
		t.Logf("restore %s: %d bytes", name, stats.Bytes)
	}
}

func assertStreamRestored(t *testing.T, nc *nats.Conn, js jetstream.JetStream, want streamFixture) {
	t.Helper()
	if want.consumer.name == "watcher" {
		settlePushDelivery(t, nc, want.name+".deliver")
	}
	got := readConsumerSnapshot(t, nc, consumerRef{stream: want.name, name: want.consumer.name})
	if got != want.consumer {
		t.Fatalf("%s consumer %s = %+v, want %+v", want.name, want.consumer.name, got, want.consumer)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := js.Stream(ctx, want.name)
	if err != nil {
		t.Fatalf("stream %s after restore: %v", want.name, err)
	}
	info, err := s.Info(ctx)
	if err != nil {
		t.Fatalf("stream info %s after restore: %v", want.name, err)
	}
	if int(info.State.Msgs) != want.published {
		t.Fatalf("%s restored message count = %d, want %d", want.name, info.State.Msgs, want.published)
	}
}

func assertKVRestored(t *testing.T, js jetstream.JetStream) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kv, err := js.KeyValue(ctx, kvBucket)
	if err != nil {
		t.Fatalf("kv bucket after restore: %v", err)
	}
	for _, key := range []string{"alpha", "beta", "gamma"} {
		if _, err := kv.Get(ctx, key); err != nil {
			t.Fatalf("kv get %s after restore: %v", key, err)
		}
	}
}

// assertDualCredentialConnect proves a single client configured with both
// a password and a JWT+seed connects unmodified to either cluster mode.
func assertDualCredentialConnect(t *testing.T, cluster *migrateCluster, busJWT, busSeed string) {
	t.Helper()
	nc, err := nats.Connect(cluster.clientURL(),
		nats.UserInfo("bus", migratePassword), nats.UserJWTAndSeed(busJWT, busSeed), nats.Timeout(5*time.Second))
	if err != nil {
		t.Fatalf("dual-credential connect: %v", err)
	}
	nc.Close()
}

// settlePushDelivery binds a subscriber to a push consumer's deliver
// subject just long enough for it to catch up: a push consumer only
// resumes pushing once something is listening, so right after a restore
// its num_pending stays stale at the ack floor until this happens.
func settlePushDelivery(t *testing.T, nc *nats.Conn, deliver string) {
	t.Helper()
	sub, err := nc.SubscribeSync(deliver)
	if err != nil {
		t.Fatalf("subscribe to settle push delivery on %s: %v", deliver, err)
	}
	defer func() { _ = sub.Unsubscribe() }()
	for {
		if _, err := sub.NextMsg(500 * time.Millisecond); err != nil {
			return
		}
	}
}

func listStreamNames(t *testing.T, js jetstream.JetStream) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var names []string
	lister := js.StreamNames(ctx)
	for name := range lister.Name() {
		names = append(names, name)
	}
	if err := lister.Err(); err != nil {
		t.Fatalf("list stream names: %v", err)
	}
	return names
}
