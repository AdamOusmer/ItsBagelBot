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
	if got.method != http.MethodGet {
		t.Fatalf("method = %s", got.method)
	}
	// The limit is clamped to Discord's own page maximum: a larger one is
	// rejected outright, not clamped by Discord.
	if got.path != "/channels/c1/messages" {
		t.Fatalf("path = %s", got.path)
	}
	if len(page) != 2 {
		t.Fatalf("page = %+v", page)
	}
	if page[0].Author.DisplayName() != "Ada L" || page[1].Author.DisplayName() != "bob" {
		t.Fatalf("display names = %q, %q", page[0].Author.DisplayName(), page[1].Author.DisplayName())
	}
	if len(page[0].Attachments) != 1 || page[0].Attachments[0].URL != "https://cdn/x.png" {
		t.Fatalf("attachments = %+v", page[0].Attachments)
	}
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
	cases := []struct {
		name    string
		parent  *string
		want    string
		absent  bool
		wantNil bool
	}{
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
			if got.method != http.MethodPatch || got.path != "/channels/c1" {
				t.Fatalf("%s %s", got.method, got.path)
			}
			if tc.absent {
				if contains(got.body, "parent_id") {
					t.Fatalf("body %q must not mention parent_id", got.body)
				}
				return
			}
			if !contains(got.body, tc.want) {
				t.Fatalf("body %q missing %q", got.body, tc.want)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
