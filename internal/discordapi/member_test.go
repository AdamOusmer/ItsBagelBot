// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi_test

import (
	"context"
	"testing"

	api "ItsBagelBot/internal/discordapi"
)

func TestRequireGuildChannel(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		allowed    bool
	}{
		{"same guild", `{"id":"c1","guild_id":"g1"}`, 200, true},
		{"thread", `{"id":"c1","guild_id":"g1","type":11}`, 200, true},
		{"foreign guild", `{"id":"c1","guild_id":"g2"}`, 200, false},
		{"DM", `{"id":"c1","type":1}`, 200, false},
		{"wrong id", `{"id":"c2","guild_id":"g1"}`, 200, false},
		{"missing", `{}`, 404, false},
		{"lookup failure", `{}`, 500, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := fakeDiscord(tc.status, tc.body)
			err := api.RequireGuildChannel(context.Background(), client, "g1", "c1")
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
		})
	}
}
