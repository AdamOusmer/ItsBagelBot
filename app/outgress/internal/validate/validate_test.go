// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package validate

import "testing"

func TestBroadcasterID(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"", false},                     // empty is not a valid id here; callers decide
		{"1", true},                     // smallest real shape
		{"123456789", true},             // typical Twitch id
		{"9999999999999999999", true},   // widest int64
		{"99999999999999999999", false}, // 20 digits: beyond anything minted
		{"12a4", false},
		{"1e3", false},
		{"-1", false},
		{" 12", false},
		{"12 ", false},
		{"1.2", false},
		{"../ratelimit:chat:x", false},
		{"UCabcdefgh", false},
		{"١٢٣", false}, // arabic-indic digits: bytes outside ASCII 0-9
	}
	for _, tc := range cases {
		if got := BroadcasterID(tc.id); got != tc.want {
			t.Errorf("BroadcasterID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}
