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
	registry := newMembershipRegistryTest(t)

	now := time.Now()
	alive := []Member{{PodID: "pod-a", Region: "node2"}, {PodID: "pod-b", Region: "worker1"}}
	registry.heartbeat(now, time.Minute, alive...)

	// A pod that stopped refreshing: its presence already lies in the past.
	stale := Member{PodID: "pod-dead", Region: "node2"}
	registry.heartbeat(now.Add(-time.Hour), time.Minute, stale)

	registry.assertMembers(now, alive, "stale pod must be excluded")
	registry.assertCardinality(2)

	// Graceful shutdown deregisters immediately instead of waiting out the ttl.
	registry.remove(alive[0])
	registry.assertMembers(now, alive[1:], "after remove")
}

type membershipRegistryTest struct {
	t      *testing.T
	ctx    context.Context
	client valkey.Client
	lc     *LeaseClient
}

func newMembershipRegistryTest(t *testing.T) *membershipRegistryTest {
	t.Helper()
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
	t.Cleanup(client.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	registry := &membershipRegistryTest{t: t, ctx: ctx, client: client, lc: NewLeaseClient(client)}
	registry.clear()
	return registry
}

func (r *membershipRegistryTest) clear() {
	r.t.Helper()
	r.mustClear()
	r.t.Cleanup(r.mustClear)
}

func (r *membershipRegistryTest) mustClear() {
	r.t.Helper()
	if err := r.client.Do(context.Background(), r.client.B().Del().Key(memberSetKey).Build()).Error(); err != nil {
		r.t.Fatalf("clear membership registry: %v", err)
	}
}

func (r *membershipRegistryTest) heartbeat(now time.Time, ttl time.Duration, members ...Member) {
	r.t.Helper()
	for _, m := range members {
		if err := r.lc.Heartbeat(r.ctx, m, now, ttl); err != nil {
			r.t.Fatalf("heartbeat %s: %v", m.PodID, err)
		}
	}
}

func (r *membershipRegistryTest) assertMembers(now time.Time, want []Member, reason string) {
	r.t.Helper()
	got, err := r.lc.ListMembers(r.ctx, now)
	if err != nil {
		r.t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		r.t.Fatalf("members = %v, want %v (%s)", got, want, reason)
	}
}

func (r *membershipRegistryTest) assertCardinality(want int64) {
	r.t.Helper()
	got, err := r.client.Do(r.ctx, r.client.B().Zcard().Key(memberSetKey).Build()).AsInt64()
	if err != nil {
		r.t.Fatal(err)
	}
	if got != want {
		r.t.Fatalf("set cardinality = %d, want %d after prune", got, want)
	}
}

func (r *membershipRegistryTest) remove(member Member) {
	r.t.Helper()
	if err := r.lc.RemoveMember(r.ctx, member); err != nil {
		r.t.Fatal(err)
	}
}
