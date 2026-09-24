// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkguard

import (
	"context"
	"testing"
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

func TestObserveSingleGuildTripDoesNotPromoteFleetWide(t *testing.T) {
	g, ctx := newTestGuarder(t)

	trip := tripGuild(ctx, g, "g1", "owner1")
	if trip.Allow {
		t.Fatalf("g1 did not trip locally (verdict %+v)", trip)
	}
	if trip.FleetPromoted {
		t.Fatalf("a single owner's trip promoted the link fleet-wide: %+v", trip)
	}

	v := g.Observe(ctx, sightingOwner(sightingArgs{Guild: "g2", Channel: "c1", User: "u1", Owner: "owner2"}))
	if !v.Allow {
		t.Fatalf("g2's first-ever sighting was blocked by g1's local trip: %+v", v)
	}
	if v.FleetHit {
		t.Fatalf("g2 saw a fleet hit after only one owner ever tripped: %+v", v)
	}
}

func TestObserveSameOwnerTwoGuildsDoesNotPromote(t *testing.T) {
	g, ctx := newTestGuarder(t)

	tripGuild(ctx, g, "g1", "sharedOwner")
	trip2 := tripGuild(ctx, g, "g2", "sharedOwner")

	if trip2.FleetPromoted {
		t.Fatalf("two guilds sharing one owner promoted the link fleet-wide: %+v", trip2)
	}
	if trip2.CorroboratingOwners != 1 {
		t.Fatalf("CorroboratingOwners = %d, want 1 (one owner, regardless of guild count)", trip2.CorroboratingOwners)
	}

	v := g.Observe(ctx, sightingOwner(sightingArgs{Guild: "g3", Channel: "c1", User: "u1", Owner: "unrelatedOwner"}))
	if !v.Allow || v.FleetHit {
		t.Fatalf("link was promoted despite only one distinct owner ever corroborating: %+v", v)
	}
}

func TestObserveTwoDistinctOwnersPromote(t *testing.T) {
	g, ctx := newTestGuarder(t)

	trip1 := tripGuild(ctx, g, "g1", "owner1")
	if trip1.FleetPromoted {
		t.Fatalf("1st owner alone promoted the link: %+v", trip1)
	}

	trip2 := tripGuild(ctx, g, "g2", "owner2")
	if !trip2.GuildTripped {
		t.Fatalf("g2 did not trip locally: %+v", trip2)
	}
	if !trip2.FleetPromoted {
		t.Fatalf("2nd independent owner's trip did not promote the link fleet-wide: %+v", trip2)
	}
	if trip2.CorroboratingOwners != FleetOwnerThreshold {
		t.Errorf("CorroboratingOwners = %d, want %d", trip2.CorroboratingOwners, FleetOwnerThreshold)
	}
}

func TestObservePromotedLinkActionedInUnseenThirdGuild(t *testing.T) {
	g, ctx := newTestGuarder(t)

	tripGuild(ctx, g, "g1", "owner1")
	promo := tripGuild(ctx, g, "g2", "owner2")
	if !promo.FleetPromoted {
		t.Fatalf("setup: link never promoted: %+v", promo)
	}

	v := g.Observe(ctx, sighting(sightingArgs{Guild: "g3", Channel: "brand-new-channel", User: "brand-new-user"}))
	if v.Allow {
		t.Fatalf("promoted link allowed through an unrelated guild that never saw it: %+v", v)
	}
	if !v.FleetHit {
		t.Errorf("FleetHit = false, want true: %+v", v)
	}
	if v.Reason != ReasonFleetPromoted {
		t.Errorf("Reason = %q, want %q", v.Reason, ReasonFleetPromoted)
	}
	if v.DistinctChannels != 0 || v.DistinctAuthors != 0 {
		t.Errorf("fleet hit touched g3's local counters: channels=%d authors=%d", v.DistinctChannels, v.DistinctAuthors)
	}
}

func TestObserveEmptyOwnerNeverCorroborates(t *testing.T) {
	g, ctx := newTestGuarder(t)

	trip := tripGuild(ctx, g, "g1", "")
	if trip.Allow {
		t.Fatalf("unbound guild's local trip was allowed through: %+v", trip)
	}
	if !trip.GuildTripped {
		t.Fatalf("unbound guild did not trip locally: %+v", trip)
	}
	if trip.FleetPromoted || trip.CorroboratingOwners != 0 {
		t.Fatalf("empty-owner trip contributed to fleet corroboration: %+v", trip)
	}

	link, _ := NormalizeLink(testLink)
	n, err := g.card(ctx, tripsKey(normalizedLink(link)))
	if err != nil {
		t.Fatalf("card: %v", err)
	}
	if n != 0 {
		t.Fatalf("trips set has %d member(s), want 0 -- empty OwnerID must never be written", n)
	}
}
