// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func card(title, description string, fields ...EmbedField) Embed {
	return Embed{Title: title, Description: description, Color: LiveColor, Fields: fields}
}

func crumbs(n string) EmbedField { return EmbedField{Name: "Crumbs", Value: n, Inline: true} }

func ticketAudit(closer, open, messages string) []EmbedField {
	return []EmbedField{
		{Name: "Opened by", Value: "<@u1>", Inline: true},
		{Name: "Closed by", Value: closer, Inline: true},
		{Name: "Open for", Value: open, Inline: true},
		{Name: "Messages", Value: messages, Inline: true},
	}
}

func footed(e Embed, footer string) Embed {
	e.Footer = &EmbedFooter{Text: footer}
	return e
}

func TestEmbedBuilders(t *testing.T) {
	panel := Config{
		TicketPanelTitle: "Need a hand?", TicketPanelBody: "Ping the mods.",
		TicketPanelColor: "#112233", TicketPanelButton: "Contact staff",
	}.TicketPanel()
	closed := TicketClosed{Opener: "<@u1>", Duration: 95 * time.Minute, MessageCount: 12, ChannelName: "ticket-ada-1"}
	closedByMod := closed
	closedByMod.Closer = "<@mod>"
	unnamed := closed
	unnamed.ChannelName = ""
	cases := []struct {
		name string
		got  Embed
		want Embed
	}{
		{"panel carries the streamer's copy and colour", TicketPanelEmbed(panel), footed(Embed{Title: "Need a hand?", Description: "Ping the mods.", Color: 0x112233}, "Bagel tickets")},
		{"panel falls back to the default copy", TicketPanelEmbed(Config{}.TicketPanel()), footed(card(TicketPanelTitleDefault, TicketPanelBodyDefault), "Bagel tickets")},
		{"opened ticket invites the close button", TicketOpenedEmbed(TicketOpened{Opener: "Ada"}), footed(card("Ticket", "Ada opened this ticket. Mods will reply here."), "Close with the button when you are done.")},
		{"opened ticket carries the claim", TicketOpenedEmbed(TicketOpened{Opener: "Ada", ClaimedBy: "Mod"}), footed(card("Ticket", "Ada opened this ticket. Mods will reply here."), "Claimed by Mod")},
		{"opened ticket without an opener names someone", TicketOpenedEmbed(TicketOpened{}), footed(card("Ticket", "Someone opened this ticket. Mods will reply here."), "Close with the button when you are done.")},
		{"closed ticket carries the audit fields", TicketClosedEmbed(closed), card("Ticket closed", "#ticket-ada-1 was closed.", ticketAudit("unknown", "1h 35m", "12")...)},
		{"closed ticket names its closer", TicketClosedEmbed(closedByMod), card("Ticket closed", "#ticket-ada-1 was closed.", ticketAudit("<@mod>", "1h 35m", "12")...)},
		{"closed ticket without a channel name", TicketClosedEmbed(unnamed), card("Ticket closed", "A ticket was closed.", ticketAudit("unknown", "1h 35m", "12")...)},
		{"voice room names its owner", VoiceRoomEmbed(VoiceRoom{Owner: "Ada"}), card("Ada's room", "Lock or unlock this channel with the buttons.")},
		{"voice room without an owner", VoiceRoomEmbed(VoiceRoom{}), card("Voice's room", "Lock or unlock this channel with the buttons.")},
		{"rank shows level and crumbs", RankEmbed(RankCard{Who: "Ada", Level: 2, XP: 400}), card("Rank", "Ada is level 2.", crumbs("400"))},
		{"rank without a name", RankEmbed(RankCard{Level: 1}), card("Rank", "This member is level 1.", crumbs("0"))},
		{"daily claim reports the balance", DailyEmbed(DailyCard{XP: 50, Fresh: true}), card("Daily crumbs", "Claimed. You have 50 crumbs.")},
		{"daily repeat is refused", DailyEmbed(DailyCard{Fresh: false}), card("Daily crumbs", "Already claimed today.")},
		{"level up names the member", LevelUpEmbed(LevelUp{Who: "Ada", Level: 3}), card("Level up", "Ada reached level 3.")},
		{"level up without a name", LevelUpEmbed(LevelUp{Level: 3}), card("Level up", "Someone reached level 3.")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{-time.Hour, "0m"},
		{30 * time.Second, "0m"},
		{90 * time.Second, "1m"},
		{59 * time.Minute, "59m"},
		{time.Hour + 5*time.Minute, "1h 5m"},
		{26*time.Hour + 61*time.Minute, "27h 1m"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, HumanDuration(tc.in), "HumanDuration(%v)", tc.in)
	}
}
