// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"net/http"
	"testing"

	"ItsBagelBot/app/outgress/internal/action"
	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"
)

func TestStreamEditorRoutes(t *testing.T) {
	act, ok := testActions().Lookup(outgress.TypeChannelUpdate)
	if !ok {
		t.Fatal("channel_update has no action")
	}
	if act.Kind != action.KindInternal {
		t.Fatalf("channel_update kind = %v, want Internal", act.Kind)
	}
	assertRoute(t, outgress.TypeStreamMarker, wantRoute{http.MethodPost, "/helix/streams/markers", outgress.AsBroadcaster})
	assertRoute(t, outgress.TypeCommercial, wantRoute{http.MethodPost, "/helix/channels/commercial", outgress.AsBroadcaster})
}

func TestStreamGetReply(t *testing.T) {
	meta := channelUpdateMeta{Field: "title", Locale: "en"}
	got := streamGetReply(meta, twitch.ChannelInfo{Title: "Ranked grind"})
	if got != "The current title is: Ranked grind" {
		t.Fatalf("title get = %q", got)
	}
	got = streamGetReply(channelUpdateMeta{Field: "tags", Locale: "en"}, twitch.ChannelInfo{})
	if got != "No tags set." {
		t.Fatalf("empty tags = %q", got)
	}
}

func TestStreamSetReply(t *testing.T) {
	got := streamSetReply(channelUpdateMeta{Field: "game", Locale: "en", User: "alice"}, "Fortnite")
	if got != "@alice updated the game to: Fortnite" {
		t.Fatalf("game set = %q", got)
	}
}

func TestStreamDrop(t *testing.T) {
	if !streamDrop(&twitch.StatusError{Status: http.StatusBadRequest}) {
		t.Fatal("400 should drop")
	}
	if !streamDrop(&twitch.StatusError{Status: http.StatusUnauthorized}) {
		t.Fatal("401 should drop")
	}
	if streamDrop(&twitch.StatusError{Status: http.StatusTooManyRequests}) {
		t.Fatal("429 should retry")
	}
	if streamDrop(&twitch.StatusError{Status: http.StatusBadGateway}) {
		t.Fatal("502 should retry")
	}
}

func TestSplitStreamTags(t *testing.T) {
	got := splitStreamTags("English,  family friendly,")
	if len(got) != 2 || got[0] != "English" || got[1] != "family friendly" {
		t.Fatalf("got %v", got)
	}
}
