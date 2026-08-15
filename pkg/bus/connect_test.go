package bus

import (
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// Every bus connection must install the async error handler. Permission
// violations are reported out-of-band — the publish already returned nil — so
// without a handler nats.go discards them entirely, which is how a missing
// $JS.FC grant stopped both hot ingress lanes with no log line, no metric and
// no error anywhere.
func TestBaseOptionsInstallAsyncErrorHandler(t *testing.T) {
	var opts nats.Options
	for _, option := range baseOptions(connectionIdentity{name: "test"}) {
		if err := option(&opts); err != nil {
			t.Fatalf("apply option: %v", err)
		}
	}
	if opts.AsyncErrorCB == nil {
		t.Fatal("no asynchronous error handler; permission violations would be discarded silently")
	}

	core, logs := observer.New(zap.ErrorLevel)
	defer zap.ReplaceGlobals(zap.New(core))()

	// nats.go passes a nil subscription for a publish violation and puts the
	// subject in the error text, so the handler has to read it from there.
	opts.AsyncErrorCB(nil, nil, errors.New(
		`nats: permissions violation: Permissions Violation for Publish to "$JS.FC.TWITCH_INGRESS.worker_premium.abcd"`))
	// A subscribe violation carries a real subscription.
	opts.AsyncErrorCB(nil, &nats.Subscription{Subject: "twitch.ingress.event.premium"}, errors.New("nats: permissions violation"))

	entries := logs.All()
	if len(entries) != 2 {
		t.Fatalf("logged %d asynchronous errors, want 2", len(entries))
	}
	for i, want := range []string{"$JS.FC.TWITCH_INGRESS.worker_premium.abcd", "twitch.ingress.event.premium"} {
		if got := entries[i].ContextMap()["subject"]; got != want {
			t.Fatalf("logged subject = %v, want %q", got, want)
		}
	}
}

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
