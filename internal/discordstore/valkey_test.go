// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore_test

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/internal/discordstore"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

func valkeyClient(t *testing.T) valkey.Client {
	t.Helper()
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{addr},
		Password:    os.Getenv("VALKEY_TEST_PASSWORD"),
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func rawValues(t *testing.T, client valkey.Client, keys []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, key := range keys {
		raw, err := client.Do(context.Background(), client.B().Get().Key(key).Build()).ToString()
		require.NoError(t, err, key)
		out[key] = raw
	}
	return out
}

func TestValkeyStoreKeepsTicketStateInItsDocumentedFormats(t *testing.T) {
	client := valkeyClient(t)
	s := discordstore.New(client)
	ctx := context.Background()
	run := strconv.FormatInt(time.Now().UnixNano(), 36)
	guild, other, channel := discordstore.Guild{ID: "g" + run}, discordstore.Guild{ID: "o" + run}, discordstore.Channel{ID: "c" + run}
	keys := []string{
		"discord:ticket:" + channel.ID, "discord:ticket:pendingclose:" + channel.ID,
		"discord:ticketdesk:" + guild.ID, "discord:ticketdesk:" + other.ID,
	}
	t.Cleanup(func() { client.Do(ctx, client.B().Del().Key(keys...).Build()) })

	_, err := s.TrackTicket(ctx, discordstore.TicketOpen{GuildID: guild.ID, ChannelID: channel.ID, OpenerID: "u1", PanelMessageID: "m1"})
	require.NoError(t, err)
	require.NoError(t, s.ClaimTicket(ctx, discordstore.TicketClaim{GuildID: guild.ID, ChannelID: channel.ID, StaffID: "mod1"}))
	closing := discordstore.TicketClose{GuildID: guild.ID, ChannelID: channel.ID, ClosedBy: "u9", ArchivedChannelID: "a1"}
	require.NoError(t, s.MarkPendingClose(ctx, closing))
	require.NoError(t, s.RememberDesk(ctx, discordstore.DeskPanel{GuildID: guild.ID, ChannelID: channel.ID, MessageID: "m2"}))
	require.True(t, s.ClaimDesk(ctx, other), "the first claim of an unposted desk wins")
	require.False(t, s.ClaimDesk(ctx, other), "a second claim loses")

	require.Equal(t, map[string]string{
		keys[0]: guild.ID + "|u1|mod1|m1",
		keys[1]: guild.ID + "|u9|a1",
		keys[2]: channel.ID + "|m2",
		keys[3]: "1",
	}, rawValues(t, client, keys))
	ticket, ticketOK := s.Ticket(ctx, guild, channel)
	foreign, foreignOK := s.Ticket(ctx, other, channel)
	pending, pendingOK := s.PendingClose(ctx, channel)
	desk, deskOK := s.Desk(ctx, guild)
	bareDesk, bareOK := s.Desk(ctx, other)
	claimed := discordstore.Ticket{
		ChannelID: channel.ID, GuildID: guild.ID, OpenerID: "u1", ClaimedBy: "mod1",
		PanelMessageID: "m1", Status: discordstore.TicketStatusClaimed,
	}
	require.Equal(t, pair(claimed, true), pair(ticket, ticketOK))
	require.Equal(t, pair(discordstore.Ticket{}, false), pair(foreign, foreignOK), "another guild cannot read the ticket")
	require.Equal(t, pair(closing, true), pair(pending, pendingOK))
	require.Equal(t, pair(discordstore.DeskPanel{GuildID: guild.ID, ChannelID: channel.ID, MessageID: "m2"}, true), pair(desk, deskOK))
	require.Equal(t, pair(discordstore.DeskPanel{GuildID: other.ID}, true), pair(bareDesk, bareOK), "a bare claim is a desk without a panel")
}

func TestValkeyStoreCachesMessagesAndFactsWithTTLs(t *testing.T) {
	client := valkeyClient(t)
	s := discordstore.New(client)
	ctx := context.Background()
	run := strconv.FormatInt(time.Now().UnixNano(), 36)
	msg := discordstore.CachedMessage{
		ID: "m" + run, GuildID: "g" + run, ChannelID: "c1", AuthorID: "u1", AuthorName: "Ada",
		Content: strings.Repeat("x", 2000), Attachments: []string{"https://cdn/a.png"},
	}
	member := discordstore.Member{GuildID: "g" + run, UserID: "u1"}
	role := discordstore.LabelRef{Kind: discordstore.LabelRole, GuildID: "g" + run, ID: "r1"}
	keys := []string{"discord:msg:" + msg.ID, "discord:mroles:g" + run + ":u1", "discord:role:g" + run + ":r1"}
	t.Cleanup(func() { client.Do(ctx, client.B().Del().Key(keys...).Build()) })

	require.NoError(t, s.RememberMessage(ctx, msg))
	require.NoError(t, s.RememberRoles(ctx, discordstore.MemberRoles{Member: member, Roles: []string{"r1", "r2"}}))
	require.NoError(t, s.RememberLabel(ctx, discordstore.Label{Ref: role, Name: "Mods"}))

	got, ok := s.RecallMessage(ctx, discordstore.Message{ID: msg.ID})
	require.True(t, ok)
	require.Len(t, []rune(got.Content), 1024)
	msg.Content = got.Content
	require.Equal(t, msg, got)
	roles, rolesOK := s.RecallRoles(ctx, member)
	require.Equal(t, pair([]string{"r1", "r2"}, true), pair(roles, rolesOK))
	name, nameOK := s.RecallLabel(ctx, role)
	require.Equal(t, pair("Mods", true), pair(name, nameOK))
	_, missing := s.RecallMessage(ctx, discordstore.Message{ID: "none" + run})
	require.False(t, missing)

	wantTTL := []time.Duration{time.Hour, 24 * time.Hour, 24 * time.Hour}
	for i, key := range keys {
		ttl, err := client.Do(ctx, client.B().Ttl().Key(key).Build()).AsInt64()
		require.NoError(t, err, key)
		require.InDelta(t, wantTTL[i].Seconds(), float64(ttl), 5, key)
	}
}
