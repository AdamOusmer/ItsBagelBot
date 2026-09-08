// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import "testing"

const testUUID = "deadbeefdeadbeefdeadbeefdeadbeef"

// resolveAccount's fallback chain, one row per priority rule: typed arg,
// linked uuid (only when PreferUUID and one is stored), linked name,
// broadcaster login. LinkedOnly drops the typed arg silently on every row.
func TestResolveAccount(t *testing.T) {
	cases := []struct {
		name string
		in   accountSources
		want string
	}{
		{"prefers stored uuid", accountSources{
			Linked: "Feinberg", LinkedUUID: testUUID, BroadcasterLogin: "streamer", PreferUUID: true,
		}, testUUID},
		{"typed arg beats uuid", accountSources{
			Arg: "@Other", Linked: "Feinberg", LinkedUUID: testUUID, BroadcasterLogin: "streamer", PreferUUID: true,
		}, "Other"},
		{"name when uuid unwanted", accountSources{
			Linked: "Feinberg", LinkedUUID: testUUID, BroadcasterLogin: "streamer",
		}, "Feinberg"},
		{"falls back to name without uuid", accountSources{
			Linked: "Feinberg", BroadcasterLogin: "streamer", PreferUUID: true,
		}, "Feinberg"},
		{"linked only ignores arg", accountSources{
			Arg: "@Other extra", Linked: "Feinberg", BroadcasterLogin: "streamer", LinkedOnly: true,
		}, "Feinberg"},
		{"linked only falls back to broadcaster", accountSources{
			Arg: "Other", BroadcasterLogin: "streamer", LinkedOnly: true,
		}, "streamer"},
	}
	for _, tc := range cases {
		if got := resolveAccount(tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
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
