// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkguard

import (
	"context"
	"testing"

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
	return New(newFakeValkey(t).client), context.Background()
}

func TestObserveBelowChannelThresholdAllows(t *testing.T) {
	g, ctx := newTestGuarder(t)

	for i, ch := range []string{"c1", "c2"} {
		v := g.Observe(ctx, sighting(sightingArgs{Guild: "g1", Channel: ch, User: "u1"}))
		if !v.Allow {
			t.Fatalf("post %d: Allow = false, want true (verdict %+v)", i, v)
		}
		if v.Reason != ReasonBelowThreshold {
			t.Errorf("post %d: Reason = %q, want %q", i, v.Reason, ReasonBelowThreshold)
		}
	}
}

func TestObserveAtChannelThresholdTrips(t *testing.T) {
	g, ctx := newTestGuarder(t)

	g.Observe(ctx, sighting(sightingArgs{Guild: "g1", Channel: "c1", User: "u1"}))
	g.Observe(ctx, sighting(sightingArgs{Guild: "g1", Channel: "c2", User: "u1"}))
	v := g.Observe(ctx, sighting(sightingArgs{Guild: "g1", Channel: "c3", User: "u1"}))

	if v.Allow {
		t.Fatalf("3rd distinct channel: Allow = true, want false (verdict %+v)", v)
	}
	if v.Reason != ReasonChannelThreshold {
		t.Errorf("Reason = %q, want %q", v.Reason, ReasonChannelThreshold)
	}
	if v.DistinctChannels != ChannelThreshold {
		t.Errorf("DistinctChannels = %d, want %d", v.DistinctChannels, ChannelThreshold)
	}
	if !v.GuildTripped {
		t.Errorf("GuildTripped = false, want true")
	}
}

func TestObserveRepeatedChannelDoesNotDoubleCount(t *testing.T) {
	g, ctx := newTestGuarder(t)

	for i := 0; i < 5; i++ {
		v := g.Observe(ctx, sighting(sightingArgs{Guild: "g1", Channel: "c1", User: "u1"}))
		if !v.Allow {
			t.Fatalf("post %d in the same channel tripped a channel-count threshold (verdict %+v)", i, v)
		}
		if v.DistinctChannels != 1 {
			t.Errorf("post %d: DistinctChannels = %d, want 1", i, v.DistinctChannels)
		}
	}
}

func TestObserveMultiAuthorLowerThresholdTrips(t *testing.T) {
	g, ctx := newTestGuarder(t)

	v1 := g.Observe(ctx, sighting(sightingArgs{Guild: "g1", Channel: "c1", User: "u1"}))
	if !v1.Allow {
		t.Fatalf("first author: Allow = false, want true (verdict %+v)", v1)
	}

	v2 := g.Observe(ctx, sighting(sightingArgs{Guild: "g1", Channel: "c1", User: "u2"}))
	if v2.Allow {
		t.Fatalf("2nd distinct author: Allow = true, want false (verdict %+v)", v2)
	}
	if v2.Reason != ReasonAuthorThreshold {
		t.Errorf("Reason = %q, want %q", v2.Reason, ReasonAuthorThreshold)
	}
	if v2.DistinctChannels != 1 {
		t.Errorf("DistinctChannels = %d, want 1 (only ever posted in c1)", v2.DistinctChannels)
	}
}

func TestObserveWindowExpiryResetsCount(t *testing.T) {
	fv := newFakeValkey(t)
	g := New(fv.client)
	ctx := context.Background()

	g.Observe(ctx, sighting(sightingArgs{Guild: "g1", Channel: "c1", User: "u1"}))
	g.Observe(ctx, sighting(sightingArgs{Guild: "g1", Channel: "c2", User: "u1"}))

	fv.advance(Window + 1)

	v := g.Observe(ctx, sighting(sightingArgs{Guild: "g1", Channel: "c3", User: "u1"}))
	if !v.Allow {
		t.Fatalf("post after window expiry tripped early (verdict %+v)", v)
	}
	if v.DistinctChannels != 1 {
		t.Errorf("DistinctChannels after window rollover = %d, want 1", v.DistinctChannels)
	}
}

func exemptionCases() []struct {
	name   string
	user   string
	setup  func(*Sighting)
	reason string
} {
	return []struct {
		name   string
		user   string
		setup  func(*Sighting)
		reason string
	}{
		{"own guild invite", "u1", func(s *Sighting) { s.OwnGuildInvite = true }, ReasonOwnInvite},
		{"moderator", "mod1", func(s *Sighting) { s.Moderator = true }, ReasonModerator},
		{"allow listed", "u1", func(s *Sighting) { s.Allowed = true }, ReasonAllowListed},
	}
}

func TestObserveExemptions(t *testing.T) {
	for _, tc := range exemptionCases() {
		t.Run(tc.name, func(t *testing.T) {
			g, ctx := newTestGuarder(t)

			for i, ch := range []string{"c1", "c2", "c3", "c4"} {
				s := sighting(sightingArgs{Guild: "g1", Channel: ch, User: tc.user})
				tc.setup(&s)
				v := g.Observe(ctx, s)
				if !v.Allow {
					t.Fatalf("post %d: %s not exempt (verdict %+v)", i, tc.name, v)
				}
				if v.Reason != tc.reason {
					t.Errorf("post %d: Reason = %q, want %q", i, v.Reason, tc.reason)
				}
			}
		})
	}
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
