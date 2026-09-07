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
