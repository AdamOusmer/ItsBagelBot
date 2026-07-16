package bus

import "testing"

// The JetStream plane must dial the hub directly while the RPC plane stays
// leaf-first. In production NATS_LEAF_URL is set, so serverList prefers the leaf;
// busURL must override that with NATS_HUB_URL, or the 150k firehose pays a leaf
// forwarding hop on every message.
func TestBusURLPrefersHubWhenSet(t *testing.T) {
	t.Setenv("NATS_HUB_URL", "nats://nats:4222")
	t.Setenv("NATS_LEAF_URL", "nats://nats-leaf:4222")

	// The override (NATS_URL) and the leaf are both ignored: JetStream is hub-only.
	if got := busURL("nats://nats-leaf:4222"); got != "nats://nats:4222" {
		t.Fatalf("busURL = %q, want hub-only nats://nats:4222", got)
	}
}

func TestBusURLFallsBackWhenNoHub(t *testing.T) {
	t.Setenv("NATS_HUB_URL", "")

	// Leaf configured, no hub: fall back to the leaf-first serverList.
	t.Setenv("NATS_LEAF_URL", "nats://nats-leaf:4222")
	if got := busURL("ignored"); got != "nats://nats-leaf:4222" {
		t.Fatalf("busURL = %q, want leaf fallback", got)
	}

	// No split at all (local dev): honor the single endpoint the caller passes.
	t.Setenv("NATS_LEAF_URL", "")
	if got := busURL("nats://127.0.0.1:4222"); got != "nats://127.0.0.1:4222" {
		t.Fatalf("busURL = %q, want local override", got)
	}
}

func TestBusPublishURLPrefersNodeLocalOverride(t *testing.T) {
	t.Setenv("NATS_HUB_URL", "nats://nats-1.nats-headless:4222")
	t.Setenv("NATS_HUB_PUBLISH_URL", "nats://nats:4222")

	if got := busPublishURL("ignored"); got != "nats://nats:4222" {
		t.Fatalf("busPublishURL = %q, want node-local hub Service", got)
	}
}

// RPC stays leaf-only even once NATS_HUB_URL is set. The leaf Service handles
// cross-node failover, while the hub is reserved for streams.
func TestRPCServerListStaysOnLeaf(t *testing.T) {
	t.Setenv("NATS_LEAF_URL", "nats://nats-leaf:4222")
	t.Setenv("NATS_HUB_URL", "nats://nats:4222")

	if got := serverList("nats://nats-rpc:4222"); got != "nats://nats-leaf:4222" {
		t.Fatalf("serverList = %q, want leaf-only RPC endpoint", got)
	}
}
