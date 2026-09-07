// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestListMessagesFullDecodesAuthorsAndAttachments(t *testing.T) {
	client, got := recording(t, 200, `[
	  {"id":"m2","content":"bye","timestamp":"2026-01-02T03:04:05.000000+00:00",
	   "author":{"id":"u1","username":"ada","global_name":"Ada L"},
	   "attachments":[{"url":"https://cdn/x.png","filename":"x.png"}]},
	  {"id":"m1","content":"hi","timestamp":"not-a-time","author":{"id":"u2","username":"bob"}}
	]`)

	page, err := client.ListMessagesFull(context.Background(), MessagePage{ChannelID: "c1", Before: "m3", Limit: 250})
	if err != nil {
		t.Fatalf("ListMessagesFull: %v", err)
	}
	// The limit is clamped to Discord's own page maximum: a larger one is
	// rejected outright, not clamped by Discord.
	wantRequest(t, got, http.MethodGet, "/channels/c1/messages")
	if len(page) != 2 {
		t.Fatalf("page = %+v", page)
	}
	wantMessages(t, page, []wantMessage{
		{displayName: "Ada L", attachmentURL: "https://cdn/x.png", timeParses: true},
		{displayName: "bob"},
	})
}

// wantMessage is everything one decoded message must show.
//
// The three checks travel as one case rather than as three helpers each
// taking a loose string, because they are assertions about the SAME message:
// split up, a reader has to re-pair "second" with page[1] by counting
// arguments, and adding a fourth field to a message means adding a fourth
// helper with its own indexing convention.
type wantMessage struct {
	// displayName pins the author fallback: global_name when Discord sent
	// one, the username otherwise.
	displayName string
	// attachmentURL is the one attachment the message carried; empty means it
	// carried none.
	attachmentURL string
	// timeParses holds the degradation rule: a malformed timestamp costs that
	// message its own time, not the whole page.
	timeParses bool
}

// wantMessages checks a decoded page against one case per message.
func wantMessages(t *testing.T, page []FullMessage, want []wantMessage) {
	t.Helper()
	if len(page) != len(want) {
		t.Fatalf("page = %d messages, want %d", len(page), len(want))
	}
	for i, w := range want {
		wantMessageDecoded(t, page[i], w)
	}
}

// wantMessageDecoded checks one message against its case.
func wantMessageDecoded(t *testing.T, msg FullMessage, want wantMessage) {
	t.Helper()
	if msg.Author.DisplayName() != want.displayName {
		t.Fatalf("display name = %q, want %q", msg.Author.DisplayName(), want.displayName)
	}
	wantAttachment(t, msg, want)
	if parsed := !msg.At().IsZero(); parsed != want.timeParses {
		t.Fatalf("timestamp parsed = %v, want %v", parsed, want.timeParses)
	}
}

// wantAttachment pins the attachments one message carried.
func wantAttachment(t *testing.T, msg FullMessage, want wantMessage) {
	t.Helper()
	if want.attachmentURL == "" {
		if len(msg.Attachments) != 0 {
			t.Fatalf("attachments = %+v, want none", msg.Attachments)
		}
		return
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("attachments = %+v, want one", msg.Attachments)
	}
	if msg.Attachments[0].URL != want.attachmentURL {
		t.Fatalf("attachment url = %q, want %q", msg.Attachments[0].URL, want.attachmentURL)
	}
}

func TestMessagePagePathClampsAndCarriesTheCursor(t *testing.T) {
	cases := []struct {
		name string
		page MessagePage
		want string
	}{
		{"clamps over the maximum", MessagePage{ChannelID: "c1", Limit: 500}, "/channels/c1/messages?limit=100"},
		{"clamps zero", MessagePage{ChannelID: "c1"}, "/channels/c1/messages?limit=100"},
		{"carries before", MessagePage{ChannelID: "c1", Limit: 20, Before: "m9"}, "/channels/c1/messages?before=m9&limit=20"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.page.path(); got != tc.want {
				t.Fatalf("path = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestModifyChannelParentIDDistinguishesUnsetFromNull(t *testing.T) {
	empty := ""
	target := "cat1"
	cases := []parentCase{
		{name: "nil leaves the category alone", parent: nil, absent: true},
		{name: "empty moves out of every category", parent: &empty, want: `"parent_id":null`},
		{name: "an id moves under it", parent: &target, want: `"parent_id":"cat1"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, got := recording(t, 200, `{}`)
			err := client.ModifyChannel(context.Background(),
				ChannelPatch{ID: "c1", Name: "closed-ticket-ada-1", ParentID: tc.parent})
			if err != nil {
				t.Fatalf("ModifyChannel: %v", err)
			}
			wantRequest(t, got, http.MethodPatch, "/channels/c1")
			wantParentID(t, got, tc)
		})
	}
}

// parentCase is one ParentID shape and what the encoded body must show for it.
type parentCase struct {
	name   string
	parent *string
	want   string
	// absent is the nil case: parent_id must not appear in the body AT ALL.
	// An explicit null is a different instruction to Discord (move the
	// channel out of every category), so "unset" cannot be encoded as null.
	absent bool
}

// wantParentID checks the recorded request body against one parentCase.
func wantParentID(t *testing.T, got *capture, tc parentCase) {
	t.Helper()
	if tc.absent {
		if strings.Contains(got.body, "parent_id") {
			t.Fatalf("body %q must not mention parent_id", got.body)
		}
		return
	}
	if !strings.Contains(got.body, tc.want) {
		t.Fatalf("body %q missing %q", got.body, tc.want)
	}
}
