// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
)

const forbiddenBody = `{"error":"Forbidden","status":403,"message":"subscription missing proper authorization"}`

func forbidden() error {
	return &twitch.StatusError{Status: http.StatusForbidden, Op: "eventsub create", Body: forbiddenBody}
}

// TestIsChatBanned pins the position-based classification: the same 403
// body is a chat ban on channel.chat.message and a lost consent anywhere
// else. Incident 2026-09-09: a banned bot was filed as revoked.
func TestIsChatBanned(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		banned     bool
		authFailed bool
	}{
		{"403 on chat", &createError{subType: twitch.ChatMessageType, err: forbidden()}, true, true},
		{"403 on a scoped sub", &createError{subType: "channel.subscribe", err: forbidden()}, false, true},
		{"403 wrapped further", fmt.Errorf("outer: %w", &createError{subType: twitch.ChatMessageType, err: forbidden()}), true, true},
		{"403 without create context", forbidden(), false, true},
		{"other 403 on chat", &createError{subType: twitch.ChatMessageType, err: &twitch.StatusError{Status: 403, Body: "nope"}}, false, false},
		{"429 on chat", &createError{subType: twitch.ChatMessageType, err: &twitch.StatusError{Status: 429, Body: forbiddenBody}}, false, false},
		{"plain error", errors.New("boom"), false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isChatBanned(tc.err); got != tc.banned {
				t.Errorf("isChatBanned = %v, want %v", got, tc.banned)
			}
			if got := isAuthRevoked(tc.err); got != tc.authFailed {
				t.Errorf("isAuthRevoked = %v, want %v", got, tc.authFailed)
			}
		})
	}
}

func TestCreateErrorKeepsTypeAndCause(t *testing.T) {
	cause := forbidden()
	err := &createError{subType: "channel.follow", err: cause}
	if !errors.Is(err, cause) {
		t.Error("createError does not unwrap to its cause")
	}
	if want := "create channel.follow: " + cause.Error(); err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestBlockedStates(t *testing.T) {
	for _, s := range []string{subStateRevoked, subStateBanned} {
		if !blockedState(s) {
			t.Errorf("blockedState(%q) = false", s)
		}
		if !reenrollableSubState(s) {
			t.Errorf("reenrollableSubState(%q) = false: a grant event must repair it", s)
		}
	}
	for _, s := range []string{subStateOK, subStateFailing, subStatePending, ""} {
		if blockedState(s) {
			t.Errorf("blockedState(%q) = true", s)
		}
	}
}

// TestAlreadyBlocked pins the once-per-outage dedupe and the precedence:
// revoked never downgrades to banned, banned upgrades to revoked.
func TestAlreadyBlocked(t *testing.T) {
	tests := []struct {
		name  string
		ch    manage.Channel
		found bool
		state string
		want  bool
	}{
		{"unknown channel", manage.Channel{}, false, subStateBanned, true},
		{"fresh pending", manage.Channel{SubState: subStatePending}, true, subStateBanned, false},
		{"already banned", manage.Channel{SubState: subStateBanned}, true, subStateBanned, true},
		{"already revoked", manage.Channel{SubState: subStateRevoked}, true, subStateRevoked, true},
		{"revoked stays over banned", manage.Channel{SubState: subStateRevoked}, true, subStateBanned, true},
		{"banned upgrades to revoked", manage.Channel{SubState: subStateBanned}, true, subStateRevoked, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := alreadyBlocked(tc.ch, tc.found, tc.state); got != tc.want {
				t.Errorf("alreadyBlocked = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLiveNoticeBanned(t *testing.T) {
	n, ok := liveNotice(manage.Channel{SubState: subStateBanned})
	if !ok || n != noticeBanned {
		t.Fatalf("liveNotice(banned) = %+v, %v; want noticeBanned", n, ok)
	}
	if n.chat != "" {
		t.Error("banned notice carries a chat line, but the chat that banned the bot cannot receive one")
	}
	n, _ = liveNotice(manage.Channel{SubState: subStateRevoked, GrantState: manage.GrantDead})
	if n != noticeRevoked {
		t.Errorf("revoked must still win over grant dead, got %+v", n)
	}
}

// Every notice needs its own request prefix (see TestNoticeRequestPrefixesDiffer
// for why); this extends the guard to the banned notice.
func TestAllNoticePrefixesDistinct(t *testing.T) {
	seen := map[string]string{}
	for name, n := range map[string]notice{"revoked": noticeRevoked, "grantDead": noticeGrantDead, "banned": noticeBanned} {
		if other, dup := seen[n.request]; dup {
			t.Errorf("%s and %s share request prefix %q", name, other, n.request)
		}
		seen[n.request] = name
	}
}
