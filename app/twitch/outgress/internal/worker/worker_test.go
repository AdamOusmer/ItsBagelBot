// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"testing"

	"ItsBagelBot/app/twitch/outgress/internal/action"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

func TestCloudBotChatActionsUseAppToken(t *testing.T) {
	actions := testActions()
	for _, typ := range []string{outgress.TypeChat, outgress.TypeAnnounce, outgress.TypeShoutout, outgress.TypePin} {
		act, ok := actions.Lookup(typ)
		if !ok {
			t.Fatalf("%s has no action", typ)
		}
		if act.As != outgress.AsApp {
			t.Fatalf("%s action identity = %q, want %q", typ, act.As, outgress.AsApp)
		}
	}
}

func testActions() action.Registry {
	return New(Config{Log: zap.NewNop()}).actions
}

func TestGeneralHelixRequestsUseTokenSpecificBuckets(t *testing.T) {
	tests := []struct {
		name       string
		message    outgress.Message
		sharedKey  string
		sharedPref string
		scope      string
		value      string
	}{
		{
			name:      "app token",
			message:   outgress.Message{As: outgress.AsApp, Endpoint: "/helix/users"},
			sharedKey: "ratelimit:helix:app",
		},
		{
			name:      "bot user token",
			message:   outgress.Message{As: outgress.AsBot, Endpoint: "/helix/moderation/bans"},
			sharedKey: "ratelimit:helix:user:bot",
		},
		{
			name:      "auto-routed bot user token",
			message:   outgress.Message{Endpoint: "/helix/moderation/channels?first=100"},
			sharedKey: "ratelimit:helix:user:bot",
		},
		{
			name:       "broadcaster user token",
			message:    outgress.Message{As: outgress.AsBroadcaster, BroadcasterID: "123", Endpoint: "/helix/clips"},
			sharedPref: "ratelimit:helix:user:", scope: "helix:user", value: "123",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, shared := generalHelixRequests(&tc.message)
			got := [4]string{shared.Key, shared.DynamicPrefix, shared.Bucket.Scope, shared.Bucket.Value}
			want := [4]string{tc.sharedKey, tc.sharedPref, tc.scope, tc.value}
			if got != want {
				t.Fatalf("shared request (key/prefix/scope/value) = %v, want %v", got, want)
			}
		})
	}
}
