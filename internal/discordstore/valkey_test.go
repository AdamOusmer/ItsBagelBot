// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore_test

import (
	"context"
	"os"
	"strconv"
	"sync"
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

func ttlOf(t *testing.T, client valkey.Client, key string) time.Duration {
	t.Helper()
	secs, err := client.Do(context.Background(), client.B().Ttl().Key(key).Build()).AsInt64()
	require.NoError(t, err, key)
	return time.Duration(secs) * time.Second
}

func TestValkeyVoiceMoveIsAtomicAndExpires(t *testing.T) {
	client := valkeyClient(t)
	s := discordstore.New(client)
	ctx := context.Background()
	run := strconv.FormatInt(time.Now().UnixNano(), 36)
	guild, room, hub := "g"+run, "room"+run, "hub"+run
	const riders = 16
	keys := []string{
		"discord:voice:" + room, "discord:voices:" + guild,
		"discord:voiceoccupants:" + room, "discord:voiceoccupants:" + hub,
	}
	for i := range riders {
		keys = append(keys, "discord:voiceseat:"+guild+":u"+strconv.Itoa(i))
	}
	t.Cleanup(func() { client.Do(ctx, client.B().Del().Key(keys...).Build()) })
	require.NoError(t, s.TrackClone(ctx, discordstore.Clone{ChannelID: room, GuildID: guild, OwnerID: "u0"}))

	joined := s.UpdateVoiceOccupancy(ctx, discordstore.VoiceSeat{GuildID: guild, UserID: "u0", ChannelID: hub})
	require.Equal(t, discordstore.VoiceMove{To: hub}, joined)
	for i := range riders {
		s.UpdateVoiceOccupancy(ctx, discordstore.VoiceSeat{GuildID: guild, UserID: "u" + strconv.Itoa(i), ChannelID: room})
	}
	for _, key := range keys[:3] {
		require.Positive(t, ttlOf(t, client, key), key)
		require.LessOrEqual(t, ttlOf(t, client, key), 24*time.Hour, key)
	}
	require.Positive(t, ttlOf(t, client, keys[4]))

	var wg sync.WaitGroup
	moves := make(chan discordstore.VoiceMove, riders)
	for i := range riders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			moves <- s.UpdateVoiceOccupancy(ctx, discordstore.VoiceSeat{GuildID: guild, UserID: "u" + strconv.Itoa(i)})
		}()
	}
	wg.Wait()
	close(moves)
	emptied := 0
	for m := range moves {
		require.Equal(t, room, m.From)
		if m.LeftEmpty {
			emptied++
		}
	}
	require.Equal(t, 1, emptied, "exactly one concurrent leaver sees the room empty")
}

func TestValkeyVoiceMoveFailsClosedWhenTheStoreIsDown(t *testing.T) {
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, Password: os.Getenv("VALKEY_TEST_PASSWORD")})
	require.NoError(t, err)
	s := discordstore.New(client)
	client.Close()

	got := s.UpdateVoiceOccupancy(context.Background(), discordstore.VoiceSeat{GuildID: "g1", UserID: "u1", ChannelID: "hub"})

	require.Equal(t, discordstore.VoiceMove{}, got)
}
