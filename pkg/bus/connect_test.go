// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

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

	opts.AsyncErrorCB(nil, nil, errors.New(
		`nats: permissions violation: Permissions Violation for Publish to "$JS.FC.TWITCH_INGRESS.worker_premium.abcd"`))
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

func TestBusURLPrefersHubWhenSet(t *testing.T) {
	t.Setenv("NATS_HUB_URL", "nats://nats:4222")
	t.Setenv("NATS_LEAF_URL", "nats://nats-leaf:4222")

	if got := busURL("nats://nats-leaf:4222"); got != "nats://nats:4222" {
		t.Fatalf("busURL = %q, want hub-only nats://nats:4222", got)
	}
}

func TestBusURLFallsBackWhenNoHub(t *testing.T) {
	t.Setenv("NATS_HUB_URL", "")

	t.Setenv("NATS_LEAF_URL", "nats://nats-leaf:4222")
	if got := busURL("ignored"); got != "nats://nats-leaf:4222" {
		t.Fatalf("busURL = %q, want leaf fallback", got)
	}

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

func TestRPCServerListStaysOnLeaf(t *testing.T) {
	t.Setenv("NATS_LEAF_URL", "nats://nats-leaf:4222")
	t.Setenv("NATS_HUB_URL", "nats://nats:4222")

	if got := serverList("nats://nats-rpc:4222"); got != "nats://nats-leaf:4222" {
		t.Fatalf("serverList = %q, want leaf-only RPC endpoint", got)
	}
}

func applyOptions(t *testing.T, opts []nats.Option) nats.Options {
	t.Helper()
	var applied nats.Options
	for _, option := range opts {
		if err := option(&applied); err != nil {
			t.Fatalf("apply option: %v", err)
		}
	}
	return applied
}

func clearCredentialEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"NATS_USER", "NATS_PASSWORD", "NATS_JWT", "NATS_NKEY_SEED",
		"NATS_RPC_USER", "NATS_RPC_PASSWORD", "NATS_RPC_JWT", "NATS_RPC_NKEY_SEED",
	} {
		t.Setenv(key, "")
	}
}

func assertUserPass(t *testing.T, applied nats.Options, wantUser, wantPass string) {
	t.Helper()
	if applied.User != wantUser || applied.Password != wantPass {
		t.Fatalf("got user=%q pass=%q, want %q/%q", applied.User, applied.Password, wantUser, wantPass)
	}
}

func assertJWT(t *testing.T, applied nats.Options, wantJWT string) {
	t.Helper()
	if applied.UserJWT == nil {
		t.Fatal("UserJWT callback not set despite both JWT env vars present")
	}
	if applied.SignatureCB == nil {
		t.Fatal("SignatureCB not set alongside UserJWT")
	}
	jwt, err := applied.UserJWT()
	if err != nil || jwt != wantJWT {
		t.Fatalf("UserJWT() = %q, %v, want %q, nil", jwt, err, wantJWT)
	}
}

func TestBusOptionsPasswordOnly(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("NATS_USER", "bus-user")
	t.Setenv("NATS_PASSWORD", "bus-pass")

	applied := applyOptions(t, busOptions("test"))
	assertUserPass(t, applied, "bus-user", "bus-pass")
	if applied.UserJWT != nil {
		t.Fatal("UserJWT callback set with no JWT env; options must stay byte-identical to today")
	}
}

func TestBusOptionsJWTOnly(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("NATS_JWT", "bus-jwt")
	t.Setenv("NATS_NKEY_SEED", "bus-seed")

	applied := applyOptions(t, busOptions("test"))
	if applied.User != "" {
		t.Fatalf("got user=%q, want empty when only JWT vars are set", applied.User)
	}
	assertJWT(t, applied, "bus-jwt")
}

func assertBusCredentialsCarryOver(t *testing.T, build func(clientName) []nats.Option) {
	t.Helper()
	clearCredentialEnv(t)
	t.Setenv("NATS_USER", "bus-user")
	t.Setenv("NATS_PASSWORD", "bus-pass")
	t.Setenv("NATS_JWT", "bus-jwt")
	t.Setenv("NATS_NKEY_SEED", "bus-seed")

	applied := applyOptions(t, build("test"))
	assertUserPass(t, applied, "bus-user", "bus-pass")
	assertJWT(t, applied, "bus-jwt")
}

func TestBusOptionsBothPasswordAndJWT(t *testing.T) {
	assertBusCredentialsCarryOver(t, busOptions)
}

func TestRPCOptionsFallsBackToBusCredentials(t *testing.T) {
	assertBusCredentialsCarryOver(t, rpcOptions)
}

func TestRPCOptionsPrefersOwnCredentials(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("NATS_USER", "bus-user")
	t.Setenv("NATS_PASSWORD", "bus-pass")
	t.Setenv("NATS_JWT", "bus-jwt")
	t.Setenv("NATS_NKEY_SEED", "bus-seed")
	t.Setenv("NATS_RPC_USER", "rpc-user")
	t.Setenv("NATS_RPC_PASSWORD", "rpc-pass")
	t.Setenv("NATS_RPC_JWT", "rpc-jwt")
	t.Setenv("NATS_RPC_NKEY_SEED", "rpc-seed")

	applied := applyOptions(t, rpcOptions("test"))
	assertUserPass(t, applied, "rpc-user", "rpc-pass")
	assertJWT(t, applied, "rpc-jwt")
}
