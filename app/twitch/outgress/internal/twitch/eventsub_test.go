// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import "testing"

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

func TestChannelSubscriptionsIncludeUserUpdate(t *testing.T) {
	var found []SubSpec
	for _, s := range ChannelSubscriptions("123", "bot") {
		if s.Type == UserUpdateType {
			found = append(found, s)
		}
	}
	if len(found) != 1 || found[0].Version != "1" || found[0].Condition["user_id"] != "123" || len(found[0].Condition) != 1 {
		t.Fatalf("user.update specs = %+v, want one v1 spec conditioned on user_id", found)
	}
	for _, s := range ControlSubscriptions(ChannelSubscriptions("123", "bot")) {
		if s.Type == UserUpdateType {
			t.Fatal("user.update must not be a control subscription")
		}
	}
}
