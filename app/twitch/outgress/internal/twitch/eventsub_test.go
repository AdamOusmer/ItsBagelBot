// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"reflect"
	"testing"
)

func TestClientSubscriptionsWatchConduitShards(t *testing.T) {
	want := []SubSpec{
		{"user.authorization.grant", "1", map[string]string{"client_id": "client"}},
		{"user.authorization.revoke", "1", map[string]string{"client_id": "client"}},
		{"conduit.shard.disabled", "1", map[string]string{"client_id": "client", "conduit_id": "conduit"}},
	}
	if got := ClientSubscriptions("client", "conduit"); !reflect.DeepEqual(got, want) {
		t.Fatalf("ClientSubscriptions = %v, want %v", got, want)
	}
}

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
		if s.BotChat() {
			t.Fatalf("%s built without a bot id", s.Type)
		}
	}
}

func TestChatNotificationReadsAsBot(t *testing.T) {
	want := map[string]string{"broadcaster_user_id": "123", "user_id": "bot"}
	for _, s := range ChannelSubscriptions("123", "bot") {
		if s.Type == ChatNotificationType {
			if !reflect.DeepEqual(s.Condition, want) || s.Version != "1" {
				t.Fatalf("notification spec = %+v", s)
			}
			return
		}
	}
	t.Fatal("no chat notification spec")
}

func userUpdateSpecs(specs []SubSpec) []SubSpec {
	var found []SubSpec
	for _, s := range specs {
		if s.Type == UserUpdateType {
			found = append(found, s)
		}
	}
	return found
}

func TestChannelSubscriptionsIncludeUserUpdate(t *testing.T) {
	found := userUpdateSpecs(ChannelSubscriptions("123", "bot"))
	if len(found) != 1 {
		t.Fatalf("user.update specs = %+v, want exactly one", found)
	}
	want := SubSpec{Type: UserUpdateType, Version: "1", Condition: map[string]string{"user_id": "123"}}
	if !reflect.DeepEqual(found[0], want) {
		t.Fatalf("user.update spec = %+v, want %+v", found[0], want)
	}
}

func TestUserUpdateIsNotAControlSubscription(t *testing.T) {
	if got := userUpdateSpecs(ControlSubscriptions(ChannelSubscriptions("123", "bot"))); len(got) != 0 {
		t.Fatalf("control subscriptions include user.update: %+v", got)
	}
}
