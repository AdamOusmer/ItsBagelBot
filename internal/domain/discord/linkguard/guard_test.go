// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkguard

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testLink = "https://discord.gg/spamcode"

type sightingArgs struct {
	Guild   string
	Channel string
	User    string
	Owner   string
}

func sighting(a sightingArgs) Sighting {
	return Sighting{GuildID: a.Guild, ChannelID: a.Channel, UserID: a.User, MessageID: "m", Link: testLink}
}

func sightingOwner(a sightingArgs) Sighting {
	s := sighting(a)
	s.OwnerID = a.Owner
	return s
}

func newTestGuarder(t *testing.T) (*Guarder, context.Context) {
	t.Helper()
	return New(valkeytest.New(t).Client()), context.Background()
}

type step struct {
	channel string
	user    string
	advance time.Duration
	want    Verdict
}

func TestObserveCountsDistinctChannelsAndAuthorsPerWindow(t *testing.T) {
	below := func(channels, authors int) Verdict {
		return Verdict{Allow: true, Reason: ReasonBelowThreshold, DistinctChannels: channels, DistinctAuthors: authors}
	}
	tripped := func(reason string, channels, authors int) Verdict {
		return Verdict{Reason: reason, DistinctChannels: channels, DistinctAuthors: authors, GuildTripped: true}
	}
	cases := []struct {
		name  string
		steps []step
	}{
		{name: "two distinct channels stay below the threshold", steps: []step{
			{channel: "c1", user: "u1", want: below(1, 1)},
			{channel: "c2", user: "u1", want: below(2, 1)},
		}},
		{name: "the third distinct channel trips the guild", steps: []step{
			{channel: "c1", user: "u1", want: below(1, 1)},
			{channel: "c2", user: "u1", want: below(2, 1)},
			{channel: "c3", user: "u1", want: tripped(ReasonChannelThreshold, ChannelThreshold, 1)},
		}},
		{name: "repeating a channel does not double count", steps: []step{
			{channel: "c1", user: "u1", want: below(1, 1)},
			{channel: "c1", user: "u1", want: below(1, 1)},
			{channel: "c1", user: "u1", want: below(1, 1)},
			{channel: "c1", user: "u1", want: below(1, 1)},
			{channel: "c1", user: "u1", want: below(1, 1)},
		}},
		{name: "a second distinct author trips at the lower author threshold", steps: []step{
			{channel: "c1", user: "u1", want: below(1, 1)},
			{channel: "c1", user: "u2", want: tripped(ReasonAuthorThreshold, 1, AuthorThreshold)},
		}},
		{name: "the count resets once the window has passed", steps: []step{
			{channel: "c1", user: "u1", want: below(1, 1)},
			{channel: "c2", user: "u1", want: below(2, 1)},
			{channel: "c3", user: "u1", advance: Window + 1, want: below(1, 1)},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := valkeytest.New(t)
			g := New(srv.Client())
			for i, st := range tc.steps {
				srv.Advance(st.advance)
				got := g.Observe(t.Context(), sighting(sightingArgs{Guild: "g1", Channel: st.channel, User: st.user}))
				want := st.want
				want.NormalizedLink, want.IsInvite = got.NormalizedLink, true
				assert.Equal(t, want, got, "post %d", i)
			}
		})
	}
}

func TestObserveExemptions(t *testing.T) {
	cases := []struct {
		name   string
		user   string
		setup  func(*Sighting)
		reason string
	}{
		{"own guild invite", "u1", func(s *Sighting) { s.OwnGuildInvite = true }, ReasonOwnInvite},
		{"moderator", "mod1", func(s *Sighting) { s.Moderator = true }, ReasonModerator},
		{"allow listed", "u1", func(s *Sighting) { s.Allowed = true }, ReasonAllowListed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, ctx := newTestGuarder(t)

			for i, ch := range []string{"c1", "c2", "c3", "c4"} {
				s := sighting(sightingArgs{Guild: "g1", Channel: ch, User: tc.user})
				tc.setup(&s)
				v := g.Observe(ctx, s)
				assert.True(t, v.Allow, "post %d: %s not exempt (verdict %+v)", i, tc.name, v)
				assert.Equal(t, tc.reason, v.Reason, "post %d", i)
			}
		})
	}
}

func TestObserveFailsOpenWhenValkeyErrors(t *testing.T) {
	srv := valkeytest.New(t)
	srv.Fail(valkeytest.Failure{Cmd: "SADD", Message: "LOADING"})
	g := New(srv.Client())

	v := g.Observe(t.Context(), sighting(sightingArgs{Guild: "g1", Channel: "c1", User: "u1"}))

	assert.True(t, v.Allow)
	assert.Equal(t, ReasonValkeyError, v.Reason)
}

func TestObserveBlankLinkIsNeverCounted(t *testing.T) {
	g, ctx := newTestGuarder(t)

	v := g.Observe(ctx, Sighting{GuildID: "g1", ChannelID: "c1", UserID: "u1", Link: "   "})

	assert.Equal(t, Verdict{Allow: true, Reason: ReasonBelowThreshold}, v)
}

func tripGuild(ctx context.Context, g *Guarder, guild, owner string) Verdict {
	var last Verdict
	for i := 0; i < ChannelThreshold; i++ {
		last = g.Observe(ctx, sightingOwner(sightingArgs{Guild: guild, Channel: "c" + string(rune('1'+i)), User: "u1", Owner: owner}))
	}
	return last
}

// Guild ownership is not an endorsement of arbitrary members' messages.
// One member can spray a rival's link in two unrelated servers, but that
// activity must never make a third server block its first sighting.
func TestObserveDifferentOwnersCannotPromoteMemberSightings(t *testing.T) {
	g, ctx := newTestGuarder(t)
	for _, owner := range []string{"owner1", "owner2"} {
		trip := tripGuild(ctx, g, owner+"-guild", owner)
		require.False(t, trip.Allow, "local spam must be blocked")
		require.True(t, trip.GuildTripped, "local spam must trip this guild")
		require.False(t, trip.FleetPromoted, "member sightings must not promote global blocking")
		require.Zero(t, trip.CorroboratingOwners, "member sightings must not endorse a link for owners")
	}
	v := g.Observe(ctx, sightingOwner(sightingArgs{Guild: "victim", Channel: "general", User: "innocent", Owner: "owner3"}))
	require.True(t, v.Allow, "other guilds must not block the victim's first sighting")
	require.False(t, v.FleetHit, "other guilds must not supply global blocking authority")
	require.Equal(t, 1, v.DistinctChannels, "the victim must keep its own local channel count")
	link, _ := NormalizeLink(testLink)
	for _, prefix := range []string{"trips:", "fleet:"} {
		n, err := g.client.Do(ctx, g.client.B().Exists().Key(keyPrefix+prefix+link).Build()).AsInt64()
		require.NoError(t, err, "checking global state %s", prefix)
		require.Zero(t, n, "ordinary sightings must not write global state %s", prefix)
	}
}

func TestObserveIgnoresLegacyFleetPromotion(t *testing.T) {
	g, ctx := newTestGuarder(t)
	link, _ := NormalizeLink(testLink)
	// Existing entries may already have been poisoned through passive
	// sightings. They must not survive the fix as blocking authority.
	err := g.client.Do(ctx, g.client.B().Hset().Key(keyPrefix+"fleet:"+link).
		FieldValue().FieldValue("owner_count", "2").Build()).Error()
	require.NoError(t, err, "seeding the legacy fleet entry")
	v := g.Observe(ctx, sighting(sightingArgs{Guild: "victim", Channel: "general", User: "innocent"}))
	require.True(t, v.Allow, "legacy fleet entries must not block unrelated guilds")
	require.False(t, v.FleetHit, "legacy fleet entries must not supply blocking authority")
	require.Equal(t, 1, v.DistinctAuthors, "the victim must keep its own local author count")
}

func TestObserveLocalDetectionDoesNotRequireOwner(t *testing.T) {
	g, ctx := newTestGuarder(t)
	trip := tripGuild(ctx, g, "g1", "")
	require.False(t, trip.Allow, "unbound guilds must still block local spam")
	require.True(t, trip.GuildTripped, "unbound guilds must still trip local detection")
	require.False(t, trip.FleetPromoted, "unbound guilds must not promote global blocking")
}
