// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import "testing"

// TestChannelSubscriptionsOrder pins the load-bearing order: the app-token
// beacon subscriptions are created before anything that can 403, and the
// chat subscription is created last so a 403 on it alone reads as a chat ban,
// not a lost consent (see worker.isChatBanned).
func TestChannelSubscriptionsOrder(t *testing.T) {
	specs := ChannelSubscriptions("123", "bot")
	if len(specs) == 0 {
		t.Fatal("no specs")
	}
	if got := specs[0].Type; got != "stream.online" {
		t.Errorf("first spec = %q, want stream.online (the go-live beacon)", got)
	}
	if got := specs[len(specs)-1].Type; got != ChatMessageType {
		t.Errorf("last spec = %q, want %s", got, ChatMessageType)
	}

	// Every app-token subscription precedes every broadcaster-scoped one.
	appToken := map[string]bool{"stream.online": true, "stream.offline": true, "channel.update": true, "channel.raid": true}
	seenScoped := false
	for _, s := range specs {
		if appToken[s.Type] && seenScoped {
			t.Errorf("app-token spec %s created after a scoped one", s.Type)
		}
		if !appToken[s.Type] {
			seenScoped = true
		}
	}
}

func TestChannelSubscriptionsOmitChatWithoutBot(t *testing.T) {
	for _, s := range ChannelSubscriptions("123", "") {
		if s.Type == ChatMessageType {
			t.Fatal("chat subscription built without a bot id")
		}
	}
}
