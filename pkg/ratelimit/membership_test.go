package ratelimit

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/valkey-io/valkey-go"
)

func TestMembershipCadence(t *testing.T) {
	tests := []struct {
		name      string
		epoch     time.Duration
		wantEvery time.Duration
		wantTTL   time.Duration
	}{
		{name: "default epoch", epoch: 30 * time.Second, wantEvery: 5 * time.Second, wantTTL: 15 * time.Second},
		{name: "tiny epoch clamps up", epoch: 6 * time.Second, wantEvery: 3 * time.Second, wantTTL: 9 * time.Second},
		{name: "huge epoch clamps down", epoch: 120 * time.Second, wantEvery: 10 * time.Second, wantTTL: 30 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			every, ttl := membershipCadence(tc.epoch)
			if every != tc.wantEvery || ttl != tc.wantTTL {
				t.Fatalf("cadence(%s) = (%s, %s), want (%s, %s)", tc.epoch, every, ttl, tc.wantEvery, tc.wantTTL)
			}
		})
	}
}

func TestEncodeDecodeMember(t *testing.T) {
	m := Member{PodID: "outgress-abc123", Region: "node2"}
	if got, ok := decodeMember(encodeMember(m)); !ok || got != m {
		t.Fatalf("round trip = %v, %v; want %v", got, ok, m)
	}
	// A separator can never appear inside a pod name or node name, so anything
	// that does not split cleanly into two non-empty halves is discarded rather
	// than admitted as a phantom member.
	for _, bad := range []string{"", "nopipe", "|only-region", "only-pod|", "|"} {
		if _, ok := decodeMember(bad); ok {
			t.Fatalf("decodeMember(%q) accepted a malformed entry", bad)
		}
	}
}

// This test is opt-in because presence expiry and pruning need a real Valkey
// sorted set, not a command mock. Run with VALKEY_TEST_ADDR.
func TestMembershipRegistryIntegration(t *testing.T) {
	address := os.Getenv("VALKEY_TEST_ADDR")
	if address == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{address},
		Password:    os.Getenv("VALKEY_TEST_PASSWORD"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lc := NewLeaseClient(client)
	// The registry key is fixed, so isolate this test by clearing it around the run.
	clear := func() { client.Do(context.Background(), client.B().Del().Key(memberSetKey).Build()) }
	clear()
	defer clear()

	now := time.Now()
	alive := []Member{{PodID: "pod-a", Region: "node2"}, {PodID: "pod-b", Region: "worker1"}}
	for _, m := range alive {
		if err := lc.Heartbeat(ctx, m, now, time.Minute); err != nil {
			t.Fatalf("heartbeat %s: %v", m.PodID, err)
		}
	}
	// A pod that stopped refreshing: its presence already lies in the past.
	stale := Member{PodID: "pod-dead", Region: "node2"}
	if err := lc.Heartbeat(ctx, stale, now.Add(-time.Hour), time.Minute); err != nil {
		t.Fatal(err)
	}

	got, err := lc.ListMembers(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, alive) {
		t.Fatalf("members = %v, want %v (stale pod must be excluded)", got, alive)
	}

	// The stale entry must have been pruned from the set, not merely filtered out.
	remaining, err := client.Do(ctx, client.B().Zcard().Key(memberSetKey).Build()).AsInt64()
	if err != nil {
		t.Fatal(err)
	}
	if remaining != 2 {
		t.Fatalf("set cardinality = %d, want 2 after prune", remaining)
	}

	// Graceful shutdown deregisters immediately instead of waiting out the ttl.
	if err := lc.RemoveMember(ctx, alive[0]); err != nil {
		t.Fatal(err)
	}
	got, err = lc.ListMembers(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != alive[1] {
		t.Fatalf("after remove members = %v, want [%v]", got, alive[1])
	}
}
