// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type found[T any] struct {
	V  T
	OK bool
}

func pair[T any](v T, ok bool) found[T] { return found[T]{V: v, OK: ok} }

func TestTicketsDurableIsFalseOnTheValkeyFallback(t *testing.T) {
	if (valkeyStore{}).TicketsDurable(context.Background()) {
		t.Fatal("the pure-Valkey fallback must not claim durable tickets")
	}
	if !NewMem().TicketsDurable(context.Background()) {
		t.Fatal("the memory double stands in for the durable path")
	}
}

func TestParseDeskValueReadsTheLegacyClaim(t *testing.T) {
	require.Equal(t, pair(DeskPanel{GuildID: "g1"}, true), pair(parseDeskValue("g1", deskClaimed)))
	require.Equal(t,
		pair(DeskPanel{GuildID: "g1", ChannelID: "c1", MessageID: "m1"}, true),
		pair(parseDeskValue("g1", "c1|m1")),
	)
}

func TestParseTicketValueReadsBothWidths(t *testing.T) {
	require.Equal(t,
		pair(Ticket{ChannelID: "c1", GuildID: "g1", OpenerID: "u1", Status: TicketStatusOpen}, true),
		pair(parseTicketValue("c1", "g1|u1")),
	)
	require.Equal(t,
		pair(Ticket{
			ChannelID: "c1", GuildID: "g1", OpenerID: "u1", ClaimedBy: "mod1",
			PanelMessageID: "m1", Status: TicketStatusClaimed,
		}, true),
		pair(parseTicketValue("c1", ticketValue(Ticket{GuildID: "g1", OpenerID: "u1", ClaimedBy: "mod1", PanelMessageID: "m1"}))),
	)
	require.Equal(t, pair(Ticket{}, false), pair(parseTicketValue("c1", "garbage")))
}
