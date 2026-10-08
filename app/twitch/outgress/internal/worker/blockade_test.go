// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import "testing"

func TestAlreadyBlockedStateSurvivesAPendingClobber(t *testing.T) {
	tests := []struct {
		name  string
		prior string
		b     blockade
		want  bool
	}{
		{"still banned across a pending retry", subStateBanned, blockBanned, true},
		{"still revoked across a pending retry", subStateRevoked, blockRevoked, true},
		{"a genuinely fresh block", subStatePending, blockBanned, false},
		{"revoked still wins over a banned retry", subStateRevoked, blockBanned, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := alreadyBlockedState(tc.prior, tc.b); got != tc.want {
				t.Errorf("alreadyBlockedState(%q) = %v, want %v", tc.prior, got, tc.want)
			}
		})
	}
}

func TestBlockedState(t *testing.T) {
	for _, s := range []string{subStateRevoked, subStateBanned} {
		if !blockedState(s) {
			t.Errorf("blockedState(%q) = false", s)
		}
	}
	for _, s := range []string{subStateOK, subStateFailing, subStatePending, ""} {
		if blockedState(s) {
			t.Errorf("blockedState(%q) = true", s)
		}
	}
}
