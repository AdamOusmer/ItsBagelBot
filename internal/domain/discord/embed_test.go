// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import "testing"

func TestCommunityEmbedsHaveTitles(t *testing.T) {
	cases := []struct {
		name  string
		embed Embed
		title string
	}{
		{"panel", TicketPanelEmbed(), "Need help?"},
		{"opened", TicketOpenedEmbed(TicketOpened{Opener: "Ada"}), "Ticket"},
		{"voice", VoiceRoomEmbed(VoiceRoom{Owner: "Ada"}), "Ada's room"},
		{"rank", RankEmbed(RankCard{Who: "Ada", Level: 2, XP: 400}), "Rank"},
		{"daily", DailyEmbed(DailyCard{XP: 50, Fresh: true}), "Daily crumbs"},
		{"daily-dup", DailyEmbed(DailyCard{Fresh: false}), "Daily crumbs"},
		{"level", LevelUpEmbed(LevelUp{Who: "Ada", Level: 1}), "Level up"},
	}
	for _, tc := range cases {
		if tc.embed.Title != tc.title {
			t.Fatalf("%s title = %q, want %q", tc.name, tc.embed.Title, tc.title)
		}
		if tc.embed.Description == "" {
			t.Fatalf("%s missing description", tc.name)
		}
	}
}
