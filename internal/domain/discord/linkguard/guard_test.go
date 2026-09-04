// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkguard

import (
	"context"
	"testing"
)

const testLink = "https://discord.gg/spamcode"

// sightingArgs is sighting's and sightingOwner's (guild, channel, user,
// owner) tuple collapsed into one struct, matching the messageEventInput/
// linkGuardRun convention used for the same shape elsewhere in this repo's
// Discord tests. sighting and sightingOwner individually took 3 and 4 bare
// string parameters -- guild id, channel id, user id, owner id are all the
// same Go type and easy to transpose at a call site -- which is what
// CodeScene's String Heavy Function Arguments flagged on this file
// (file-level).
type sightingArgs struct {
	Guild   string
	Channel string
	User    string
	Owner   string // sighting ignores this; only sightingOwner reads it.
}

// sighting builds a Sighting with no owner binding, matching a guild that
// never completed setup. That is a deliberate default for most of these
// tests: local channel/author counting and enforcement must not depend on
// OwnerID at all, only fleet corroboration does (see corroborate).
func sighting(a sightingArgs) Sighting {
	return Sighting{GuildID: a.Guild, ChannelID: a.Channel, UserID: a.User, MessageID: "m", Link: testLink}
}

// sightingOwner is sighting plus the Twitch broadcaster id the guild is
// bound to, for the fleet-corroboration tests.
func sightingOwner(a sightingArgs) Sighting {
	s := sighting(a)
	s.OwnerID = a.Owner
	return s
}

// newTestGuarder builds a Guarder backed by a fresh fake Valkey plus the
// background context: nearly every test below opened with this identical
// pair of lines, repeated so many times it was the duplication CodeScene
// flagged on this file.
func newTestGuarder(t *testing.T) (*Guarder, context.Context) {
	t.Helper()
	return New(newFakeValkey(t).client), context.Background()
}

func TestObserveBelowChannelThresholdAllows(t *testing.T) {
	g, ctx := newTestGuarder(t)

	for i, ch := range []string{"c1", "c2"} { // one under ChannelThreshold (3)
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
	v := g.Observe(ctx, sighting(sightingArgs{Guild: "g1", Channel: "c3", User: "u1"})) // 3rd distinct channel == ChannelThreshold

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
	// The same account reposting in the SAME channel is flooding, not the
	// cross-channel signal this package targets -- see the package doc's
	// "why distinct channels, not message count".
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
	// Two distinct accounts posting the identical link, in the SAME
	// channel, is the hacked-account-wave signature and must trip
	// AuthorThreshold well before ChannelThreshold (3) would ever fire.
	g, ctx := newTestGuarder(t)

	v1 := g.Observe(ctx, sighting(sightingArgs{Guild: "g1", Channel: "c1", User: "u1"}))
	if !v1.Allow {
		t.Fatalf("first author: Allow = false, want true (verdict %+v)", v1)
	}

	v2 := g.Observe(ctx, sighting(sightingArgs{Guild: "g1", Channel: "c1", User: "u2"})) // 2nd distinct author == AuthorThreshold
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

	fv.advance(Window + 1) // cross the fixed window boundary

	// A fresh post after the window rolled over must start counting from
	// scratch, not add a 3rd channel to the expired set.
	v := g.Observe(ctx, sighting(sightingArgs{Guild: "g1", Channel: "c3", User: "u1"}))
	if !v.Allow {
		t.Fatalf("post after window expiry tripped early (verdict %+v)", v)
	}
	if v.DistinctChannels != 1 {
		t.Errorf("DistinctChannels after window rollover = %d, want 1", v.DistinctChannels)
	}
}

// exemptionCases table-drives the three false-positive exemptions
// exemptVerdict applies. They used to be three separately-written test
// functions that repeated the identical "post well past ChannelThreshold,
// assert every post stays allowed with the matching reason" loop, differing
// only in which Sighting field they flipped and which reason they expected
// -- that repetition is what CodeScene flagged as duplication on this file.
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

			for i, ch := range []string{"c1", "c2", "c3", "c4"} { // well past ChannelThreshold
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

// tripGuild drives ChannelThreshold distinct channels for one guild, bound
// to owner, and returns the verdict from the trip itself.
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

	// A second, unrelated guild (and owner) seeing this link for the FIRST
	// time must be judged on its own -- not pre-blocked by g1's local
	// trip. This is the abuse case FleetOwnerThreshold exists to close:
	// one owner alone, however many guilds they control, can never get a
	// link actioned everywhere.
	v := g.Observe(ctx, sightingOwner(sightingArgs{Guild: "g2", Channel: "c1", User: "u1", Owner: "owner2"}))
	if !v.Allow {
		t.Fatalf("g2's first-ever sighting was blocked by g1's local trip: %+v", v)
	}
	if v.FleetHit {
		t.Fatalf("g2 saw a fleet hit after only one owner ever tripped: %+v", v)
	}
}

func TestObserveSameOwnerTwoGuildsDoesNotPromote(t *testing.T) {
	// The regression this whole gate exists for: one attacker standing up
	// TWO guilds (free, self-service) and tripping the local threshold in
	// both must NOT clear fleet corroboration, because both guilds are
	// bound to the SAME Twitch owner -- only one distinct owner ever
	// existed no matter how many guilds that owner puppets.
	g, ctx := newTestGuarder(t)

	tripGuild(ctx, g, "g1", "sharedOwner")
	trip2 := tripGuild(ctx, g, "g2", "sharedOwner")

	if trip2.FleetPromoted {
		t.Fatalf("two guilds sharing one owner promoted the link fleet-wide: %+v", trip2)
	}
	if trip2.CorroboratingOwners != 1 {
		t.Fatalf("CorroboratingOwners = %d, want 1 (one owner, regardless of guild count)", trip2.CorroboratingOwners)
	}

	// A third guild, bound to a genuinely different owner, must still be
	// judged fresh -- the two same-owner guilds must not have promoted it.
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

	// g3 has never seen this link before -- a single sighting, in a
	// channel it has never posted in, by a user it has never seen, from a
	// guild with no owner binding at all. It must still be actioned purely
	// off the fleet-wide promotion.
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
	// A guild that never completed setup has no verified owner. It must
	// still get full local detection and enforcement (GuildTripped,
	// Allow=false) -- only its contribution to FLEET promotion is
	// withheld, or an attacker could just skip setup to reopen the same
	// free-multiplication hole FleetOwnerThreshold exists to close.
	g, ctx := newTestGuarder(t)

	trip := tripGuild(ctx, g, "g1", "") // no owner binding
	if trip.Allow {
		t.Fatalf("unbound guild's local trip was allowed through: %+v", trip)
	}
	if !trip.GuildTripped {
		t.Fatalf("unbound guild did not trip locally: %+v", trip)
	}
	if trip.FleetPromoted || trip.CorroboratingOwners != 0 {
		t.Fatalf("empty-owner trip contributed to fleet corroboration: %+v", trip)
	}

	// Prove it directly against Valkey, not just via the returned Verdict:
	// the trips set for this link must still be empty.
	link, _ := NormalizeLink(testLink)
	n, err := g.card(ctx, tripsKey(normalizedLink(link)))
	if err != nil {
		t.Fatalf("card: %v", err)
	}
	if n != 0 {
		t.Fatalf("trips set has %d member(s), want 0 -- empty OwnerID must never be written", n)
	}
}
