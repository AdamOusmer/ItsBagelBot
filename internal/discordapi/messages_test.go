// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

import (
	"context"
	"net/http"
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
	wantDisplayNames(t, page, "Ada L", "bob")
	wantAttachment(t, page[0], "https://cdn/x.png")
	wantTimestamps(t, page)
}

// wantDisplayNames pins the author fallback: global_name when Discord sent
// one, the username otherwise.
func wantDisplayNames(t *testing.T, page []FullMessage, first, second string) {
	t.Helper()
	if page[0].Author.DisplayName() != first {
		t.Fatalf("display name = %q, want %q", page[0].Author.DisplayName(), first)
	}
	if page[1].Author.DisplayName() != second {
		t.Fatalf("display name = %q, want %q", page[1].Author.DisplayName(), second)
	}
}

// wantAttachment pins the one attachment a message carried.
func wantAttachment(t *testing.T, msg FullMessage, url string) {
	t.Helper()
	if len(msg.Attachments) != 1 {
		t.Fatalf("attachments = %+v, want one", msg.Attachments)
	}
	if msg.Attachments[0].URL != url {
		t.Fatalf("attachment url = %q, want %q", msg.Attachments[0].URL, url)
	}
}

// wantTimestamps holds the degradation rule: a malformed timestamp costs that
// message its time, not the whole page.
func wantTimestamps(t *testing.T, page []FullMessage) {
	t.Helper()
	if page[0].At().IsZero() {
		t.Fatal("a well-formed timestamp must parse")
	}
	if !page[1].At().IsZero() {
		t.Fatal("an unparseable timestamp degrades to the zero time, it does not fail the page")
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
			wantParentID(t, got.body, tc)
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

// wantParentID checks the body against one parentCase.
func wantParentID(t *testing.T, body string, tc parentCase) {
	t.Helper()
	if tc.absent {
		if contains(body, "parent_id") {
			t.Fatalf("body %q must not mention parent_id", body)
		}
		return
	}
	if !contains(body, tc.want) {
		t.Fatalf("body %q missing %q", body, tc.want)
	}
}

func contains(haystack, needle string) bool {
	if needle == "" || len(haystack) < len(needle) {
		return false
	}
	return indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
