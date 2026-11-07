// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package live

import "testing"

func TestBroadcasterIDKeepsOnlyNumericLiveKeys(t *testing.T) {
	cases := []struct {
		key string
		id  uint64
		ok  bool
	}{
		{key: "live:44322190", id: 44322190, ok: true},
		{key: "live:ver:44322190"},
		{key: "live:recheck:44322190"},
		{key: "live:0"},
		{key: "live:"},
		{key: "live:012"},
		{key: "settings:44322190"},
	}
	for _, tc := range cases {
		id, ok := BroadcasterID(tc.key)
		if ok != tc.ok || id != tc.id {
			t.Fatalf("BroadcasterID(%q) = %d, %v; want %d, %v", tc.key, id, ok, tc.id, tc.ok)
		}
	}
}
