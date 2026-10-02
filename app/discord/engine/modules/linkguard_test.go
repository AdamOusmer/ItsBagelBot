// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/app/discord/engine/modules"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/discord/linkguard"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type sightingFlags struct{ Moderator, Allowed bool }

func guardWith(trips, valkeyErrors []string) *fakeGuard {
	verdicts := map[string]linkguard.Verdict{}
	for _, link := range trips {
		norm, _ := linkguard.NormalizeLink(link)
		_, invite := linkguard.NormalizeLink(link)
		verdicts[norm] = linkguard.Verdict{
			Reason: linkguard.ReasonChannelThreshold, NormalizedLink: norm, IsInvite: invite, GuildTripped: true,
		}
	}
	for _, link := range valkeyErrors {
		norm, _ := linkguard.NormalizeLink(link)
		verdicts[norm] = linkguard.Verdict{Allow: true, Reason: linkguard.ReasonValkeyError, NormalizedLink: norm}
	}
	return &fakeGuard{verdicts: verdicts}
}

func messageEvent(t *testing.T, content string, bot bool, roles []string) []byte {
	t.Helper()
	raw, err := codec.Marshal(map[string]any{
		"id": "m1", "guild_id": "g1", "channel_id": "c3", "content": content,
		"author": map[string]any{"id": "u1", "bot": bot},
		"member": map[string]any{"roles": roles},
	})
	require.NoError(t, err)
	return raw
}

func TestLinkGuard(t *testing.T) {
	const (
		spam    = "discord.gg/spamcode"
		own     = "discord.gg/ourownserver"
		other   = "discord.gg/someoneelses"
		partner = "discord.gg/partner"
		plain   = "check out discord.gg/abc123"
	)
	cases := []struct {
		name         string
		content      string
		bot          bool
		roles        []string
		allowList    string
		trips        []string
		valkeyErrors []string
		ownLinks     map[string]bool
		ownErr       error
		wantDeletes  int
		wantSeen     []sightingFlags
		wantOwnCalls []string
	}{
		{name: "TestLinkGuardResolutionNotAttemptedForNonTrippingLink: a link below threshold is untouched and never resolved",
			content: plain, wantSeen: []sightingFlags{{}}},
		{name: "a threshold trip deletes the message with its reason",
			content: "join now " + spam, trips: []string{spam}, wantDeletes: 1, wantSeen: []sightingFlags{{}}, wantOwnCalls: []string{spam}},
		{name: "a moderator repost is exempt",
			content: spam, roles: []string{"modsrole"}, wantSeen: []sightingFlags{{Moderator: true}}},
		{name: "an allow-listed link is exempt",
			content: partner, allowList: partner, wantSeen: []sightingFlags{{Allowed: true}}},
		{name: "TestLinkGuardOwnInviteTripIsNotDeleted",
			content: own, trips: []string{own}, ownLinks: map[string]bool{own: true}, wantSeen: []sightingFlags{{}}, wantOwnCalls: []string{own}},
		{name: "TestLinkGuardOtherGuildInviteStillDeleted",
			content: other, trips: []string{other}, ownLinks: map[string]bool{other: false}, wantDeletes: 1, wantSeen: []sightingFlags{{}}, wantOwnCalls: []string{other}},
		{name: "TestLinkGuardOwnInviteRPCFailureSkipsAction",
			content: spam, trips: []string{spam}, ownErr: errors.New("outgress rpc timeout"), wantSeen: []sightingFlags{{}}, wantOwnCalls: []string{spam}},
		{name: "a bot author is ignored",
			content: spam, bot: true},
		{name: "a Valkey error fails open",
			content: spam, valkeyErrors: []string{spam}, wantSeen: []sightingFlags{{}}},
		{name: "three tripped links in one message delete once and are all recorded",
			content: "discord.gg/one discord.gg/two discord.gg/three", trips: []string{"discord.gg/one", "discord.gg/two", "discord.gg/three"},
			wantDeletes: 1, wantSeen: []sightingFlags{{}, {}, {}}, wantOwnCalls: []string{"discord.gg/one"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guard := guardWith(tc.trips, tc.valkeyErrors)
			ownInvite := &fakeOwnInvite{own: tc.ownLinks, err: tc.ownErr}
			cfg := ddiscord.Config{GuildID: "g1", ModsRoleID: "modsrole", LinkGuardEnabled: "on", LinkAllowList: tc.allowList}
			handler := modules.LinkGuard(guard, ownInvite, zap.NewNop()).Events["MESSAGE_CREATE"]
			var emitted []ddiscord.Command
			c := &module.Context{
				Event:  ddiscord.Event{Type: "MESSAGE_CREATE", GuildID: "g1", Raw: messageEvent(t, tc.content, tc.bot, tc.roles)},
				Config: cfg, BroadcasterID: "999", Log: zap.NewNop(),
			}

			require.NoError(t, handler(context.Background(), c, func(cmd ddiscord.Command) { emitted = append(emitted, cmd) }))

			require.Len(t, emitted, tc.wantDeletes, "only deletes are emitted")
			for _, cmd := range emitted {
				requireDeletesTheMessage(t, cmd)
			}
			require.Equal(t, tc.wantSeen, flagsOf(guard.seen))
			require.Equal(t, tc.wantOwnCalls, ownInvite.calls)
		})
	}
}

func flagsOf(seen []linkguard.Sighting) []sightingFlags {
	var out []sightingFlags
	for _, s := range seen {
		out = append(out, sightingFlags{Moderator: s.Moderator, Allowed: s.Allowed})
	}
	return out
}

func requireDeletesTheMessage(t *testing.T, cmd ddiscord.Command) {
	t.Helper()
	require.Equal(t, ddiscord.TypeDeleteMessage, cmd.Type)
	require.Equal(t, [2]string{"g1", "c3"}, [2]string{cmd.GuildID, cmd.ChannelID})
	require.NotEmpty(t, cmd.Reason, "the tripped threshold is recorded for the audit log")
	var payload ddiscord.DeletePayload
	require.NoError(t, codec.Unmarshal(cmd.Payload, &payload))
	require.Equal(t, "m1", payload.MessageID)
}

func TestOwnInviteChecker(t *testing.T) {
	const (
		positiveTTL = 24 * time.Hour
		negativeTTL = linkguard.Window
	)
	type lookup struct {
		guildID string
		wantOwn bool
		wantErr bool
	}
	cases := []struct {
		name      string
		link      string
		reply     discordoutgress.InviteResolveReply
		rpcErr    error
		lookups   []lookup
		wantCalls int
		wantCache map[string]cachedGuild
	}{
		{name: "TestOwnInviteResolvesAndCachesPositive", link: "discord.gg/abc",
			reply:     discordoutgress.InviteResolveReply{GuildID: "g1"},
			lookups:   []lookup{{guildID: "g1", wantOwn: true}},
			wantCalls: 1, wantCache: map[string]cachedGuild{"abc": {GuildID: "g1", TTL: positiveTTL}}},
		{name: "TestOwnInviteSecondLookupHitsCacheNotResolver", link: "discord.gg/abc",
			reply:     discordoutgress.InviteResolveReply{GuildID: "g1"},
			lookups:   []lookup{{guildID: "g1", wantOwn: true}, {guildID: "g2"}},
			wantCalls: 1, wantCache: map[string]cachedGuild{"abc": {GuildID: "g1", TTL: positiveTTL}}},
		{name: "TestOwnInviteNotFoundCachesNegative", link: "discord.gg/dead",
			reply:     discordoutgress.InviteResolveReply{NotFound: true},
			lookups:   []lookup{{guildID: "g1"}, {guildID: "g1"}},
			wantCalls: 1, wantCache: map[string]cachedGuild{"dead": {TTL: negativeTTL}}},
		{name: "TestOwnInviteRPCErrorNotCached", link: "discord.gg/abc",
			rpcErr:    errors.New("nats timeout"),
			lookups:   []lookup{{guildID: "g1", wantErr: true}, {guildID: "g1", wantErr: true}},
			wantCalls: 2, wantCache: map[string]cachedGuild{}},
		{name: "TestOwnInviteReplyErrorNotCached", link: "discord.gg/abc",
			reply:     discordoutgress.InviteResolveReply{Error: "discord: rate limited"},
			lookups:   []lookup{{guildID: "g1", wantErr: true}},
			wantCalls: 1, wantCache: map[string]cachedGuild{}},
		{name: "TestOwnInviteNonInviteLinkNeverResolves", link: "https://example.com/not-an-invite",
			lookups: []lookup{{guildID: "g1"}}, wantCache: map[string]cachedGuild{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &fakeInviteResolver{reply: tc.reply, err: tc.rpcErr}
			cache := &memInviteCache{entries: map[string]cachedGuild{}}
			checker := modules.NewOwnInviteChecker(resolver, cache)

			for _, l := range tc.lookups {
				own, err := checker.IsOwnGuildInvite(context.Background(), l.guildID, tc.link)

				require.Equal(t, l.wantErr, err != nil)
				require.Equal(t, l.wantOwn, own)
			}

			require.Equal(t, tc.wantCalls, resolver.calls)
			require.Equal(t, tc.wantCache, cache.entries)
		})
	}
}
