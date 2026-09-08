// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import "testing"

func TestResolveAccountPrefersStoredUUID(t *testing.T) {
	got := resolveAccount(accountSources{
		Linked:           "Feinberg",
		LinkedUUID:       "deadbeefdeadbeefdeadbeefdeadbeef",
		BroadcasterLogin: "streamer",
		PreferUUID:       true,
	})
	if got != "deadbeefdeadbeefdeadbeefdeadbeef" {
		t.Fatalf("linked uuid: got %q", got)
	}
}

func TestResolveAccountTypedArgBeatsUUID(t *testing.T) {
	got := resolveAccount(accountSources{
		Arg:              "@Other",
		Linked:           "Feinberg",
		LinkedUUID:       "deadbeefdeadbeefdeadbeefdeadbeef",
		BroadcasterLogin: "streamer",
		PreferUUID:       true,
	})
	if got != "Other" {
		t.Fatalf("typed arg: got %q", got)
	}
}

func TestResolveAccountNameWhenUUIDUnwanted(t *testing.T) {
	got := resolveAccount(accountSources{
		Linked:           "Feinberg",
		LinkedUUID:       "deadbeefdeadbeefdeadbeefdeadbeef",
		BroadcasterLogin: "streamer",
	})
	if got != "Feinberg" {
		t.Fatalf("prefer name: got %q", got)
	}
}

func TestResolveAccountFallsBackToNameWithoutUUID(t *testing.T) {
	got := resolveAccount(accountSources{
		Linked:           "Feinberg",
		BroadcasterLogin: "streamer",
		PreferUUID:       true,
	})
	if got != "Feinberg" {
		t.Fatalf("no uuid stored: got %q", got)
	}
}

// The broadcaster's linked-only toggle drops the typed name silently: the
// viewer gets the linked account, not a refusal.
func TestResolveAccountLinkedOnlyIgnoresArg(t *testing.T) {
	got := resolveAccount(accountSources{
		Arg:              "@Other extra",
		Linked:           "Feinberg",
		BroadcasterLogin: "streamer",
		LinkedOnly:       true,
	})
	if got != "Feinberg" {
		t.Fatalf("linked only: got %q", got)
	}
}

// Linked-only with no linked account still lands on the broadcaster's own
// login, never on the typed name.
func TestResolveAccountLinkedOnlyFallsBackToBroadcaster(t *testing.T) {
	got := resolveAccount(accountSources{
		Arg:              "Other",
		BroadcasterLogin: "streamer",
		LinkedOnly:       true,
	})
	if got != "streamer" {
		t.Fatalf("linked only, nothing linked: got %q", got)
	}
}

func TestExplicitOnDefaultsOff(t *testing.T) {
	for _, v := range []string{"", "off", "yes"} {
		if explicitOn(v) {
			t.Fatalf("%q should not count as on", v)
		}
	}
	if !explicitOn("on") {
		t.Fatal(`"on" should count as on`)
	}
}
