// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkguard

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// Tuned constants. Every threshold and window this package uses is named
// and explained here rather than scattered inline -- see decision-records
// notes throughout this file and the package doc for the reasoning behind
// each number, including what breaks if it is raised or lowered.
const (
	// ChannelThreshold trips a guild-local verdict once a normalized link
	// has appeared in this many DISTINCT channels of one guild within
	// Window. 3 was chosen because organic cross-posting of one link
	// (an announcement copied to both #announcements and #general, or a
	// pinned rules link) commonly touches 2 channels; a hijacked account
	// working through a channel list reaches a 3rd within the same burst.
	// Lower to 2 and ordinary double-posting habits start tripping; raise
	// past 3 and the attacker gets that many more channels hit, per guild,
	// before anything reacts.
	ChannelThreshold = 3

	// AuthorThreshold is the lower bar for MULTIPLE DISTINCT ACCOUNTS
	// posting the identical link at once -- the signature of a batch of
	// accounts compromised together, as opposed to one hijacked account
	// working a channel list. Two strangers independently posting the same
	// third-party invite within one Window essentially never happens
	// organically, so 2 is the floor this signal needs, not a tuned-up
	// number picked for headroom. Raising it "to be safe" defeats the
	// point of having a faster-tripping signal for the wave case.
	AuthorThreshold = 2

	// Window bounds both the channel-count and author-count sets. Each
	// set's TTL is set once, at first sighting (EXPIRE ... NX), not
	// refreshed on every later member added -- a refreshed TTL would let a
	// slow trickle of posts keep one counter alive indefinitely, and would
	// let a single popular, entirely legitimate invite accumulate phantom
	// "channels" over weeks of unrelated mentions. A fixed window closes
	// both. 10 minutes covers how fast a hijacked session actually walks a
	// channel list (seconds to low minutes) while being short enough that
	// spaced-out, unrelated organic mentions of the same link never
	// accumulate into a false trip.
	Window = 10 * time.Minute

	// FleetOwnerThreshold is the number of DISTINCT OWNERS (Twitch
	// broadcaster ids, not guild ids) that must each have independently
	// tripped ChannelThreshold or AuthorThreshold on the same normalized
	// link before the fleet promotes it. This is the single most important
	// number in the package -- see the package doc's "why cross-owner
	// corroboration gates fleet promotion".
	//
	// It corroborates on OWNER, not guild, because a Discord GUILD is free
	// to create and self-service to bind the bot to: one person can stand
	// up a second guild in seconds and trip its local threshold there too,
	// so counting distinct GUILD ids would let a single attacker clear any
	// guild-count bar at essentially zero cost -- exactly the "grief a
	// rival by spamming their own invite in your own server(s)" attack this
	// gate exists to close, just multiplied by however many free guilds
	// they bother to create. A Twitch broadcaster binding
	// (discord:guild:{id}, written during guild setup) is not free the same
	// way: it requires a separate, enrolled Twitch channel. Requiring 2+
	// DISTINCT OWNERS means the actual cost of clearing this gate is
	// "enroll a second Twitch channel with us", not "click Create Server"
	// twice.
	FleetOwnerThreshold = 2

	// FleetTTL is how long a fleet-wide promotion stands before it expires
	// and must be re-earned. Days, not forever, and enforced in Valkey
	// itself rather than left to a human to remember: a promotion applies
	// in guilds that never independently saw the link, so a bad one (a
	// stale invite that changed hands, a corroboration that turns out to
	// have been coordinated rather than independent) needs a built-in
	// expiry. 7 days covers how long the pattern this defends against
	// typically stays active (a stolen link keeps circulating for as long
	// as the compromised account or scam campaign runs, typically days)
	// without letting a stale block outlive the incident it was raised for.
	FleetTTL = 7 * 24 * time.Hour

	// CorroborationWindow bounds how long one guild's local trip counts
	// toward fleet corroboration from that guild's owner. Deliberately
	// wider than Window: Window is tuned for one account spraying one
	// guild in minutes, but the guild-to-guild (and so owner-to-owner)
	// propagation this package hunts for (the same compromised account or
	// scam link moving between servers) realistically plays out over
	// hours.
	CorroborationWindow = 24 * time.Hour
)

// Reason codes on a Verdict. Always a fixed string so a caller can switch on
// it without parsing prose; the accompanying Verdict fields carry the
// numbers behind the decision.
const (
	ReasonOwnInvite        = "own_invite"
	ReasonModerator        = "moderator"
	ReasonAllowListed      = "allow_listed"
	ReasonFleetPromoted    = "fleet_promoted"
	ReasonChannelThreshold = "channel_threshold"
	ReasonAuthorThreshold  = "author_threshold"
	ReasonBelowThreshold   = "below_threshold"
	// ReasonValkeyError marks a verdict returned when Valkey could not be
	// reached. It always Allows: a storage hiccup should never itself
	// cause an action, only cost this one sighting its count -- see
	// Observe.
	ReasonValkeyError = "valkey_error"
)

// Sighting is one message containing a link, as reported by the caller.
//
// The exemption fields, and OwnerID, are look-ups only the caller can do --
// this package makes no Discord API calls, and no lookup of its own against
// the discord:guild:{id} reverse index (see the package doc's "Scope") --
// so the caller resolves them (guild invite settings, the author's
// effective permissions, the guild's own allow-list config, the guild's
// bound Twitch broadcaster id) and reports the answer here rather than
// this package reaching out to fetch it itself.
type Sighting struct {
	GuildID   string
	ChannelID string
	UserID    string
	MessageID string
	Link      string

	// OwnerID is the Twitch broadcaster id GuildID is bound to (the
	// discord:guild:{id} reverse index written during guild setup), used
	// ONLY for fleet corroboration -- see corroborate for why owner rather
	// than guild, and what happens when this is left empty.
	OwnerID string

	// OwnGuildInvite is true when Link is GuildID's own invite. A server
	// linking itself across its own channels (rules, welcome, an event
	// announcement) is normal operation, not an attack -- it is the same
	// repetition shape but with none of the cross-boundary intent this
	// package targets.
	OwnGuildInvite bool

	// Moderator is true when UserID holds a moderation permission in
	// GuildID. Staff legitimately repost links (verification, appeals,
	// investigating a report) more often than ordinary members.
	Moderator bool

	// Allowed is true when Link matches an allow-list GuildID has
	// configured (e.g. a partner server's invite the guild intentionally
	// shares often).
	Allowed bool
}

// Verdict is advisory data: what was observed, and which threshold (if any)
// tripped. It is never an instruction -- this package decides what
// happened, the caller (which owns Discord API access and moderation
// policy) decides and performs any action.
type Verdict struct {
	// Allow is false when the caller should act on this sighting.
	Allow bool
	// Reason is always set, to one of the Reason* constants.
	Reason string

	NormalizedLink string
	IsInvite       bool

	// DistinctChannels and DistinctAuthors are this guild's counts for
	// NormalizedLink within the current Window, after recording this
	// sighting (0 for a sighting that was exempted or a fleet hit, since
	// neither path touches the per-guild counters).
	DistinctChannels int
	DistinctAuthors  int

	// GuildTripped is true when this guild independently crossed
	// ChannelThreshold or AuthorThreshold on this sighting.
	GuildTripped bool
	// FleetHit is true when this sighting matched a link the fleet had
	// already promoted -- independent of this guild's own counts, and
	// possibly a guild that has never seen the link before. See the
	// package doc's three-guild scenario.
	FleetHit bool
	// FleetPromoted is true when THIS call is the one that pushed the
	// link's cross-owner corroboration over FleetOwnerThreshold.
	FleetPromoted bool
	// CorroboratingOwners is the number of distinct Twitch broadcaster
	// owners on record as having tripped locally on NormalizedLink. Only
	// populated once this guild has itself tripped AND contributed to
	// corroboration (see corroborate -- an empty OwnerID never touches
	// this), or on a FleetHit (best-effort).
	CorroboratingOwners int
}

// allow builds a Verdict that lets the message through.
func allow(reason, link string, invite bool) Verdict {
	return Verdict{Allow: true, Reason: reason, NormalizedLink: link, IsInvite: invite}
}

// Guarder is the linkguard engine. It is safe for concurrent use; all state
// lives in Valkey, keyed as documented in the package doc.
type Guarder struct {
	client valkey.Client
}

// New builds a Guarder backed by client. client follows the same shape
// internal/discordstore and app/discord/outgress's kv package already
// depend on (github.com/valkey-io/valkey-go's Client) -- this package adds
// no new Valkey wrapper of its own.
//
// Unlike internal/discordstore.New, this constructor does NOT
// nil-guard client: a nil client is not turned into an in-memory fallback,
// and Observe WILL PANIC on first use if client is nil. Whoever wires this
// package in must either always pass a live client or add their own
// nil check before calling New.
func New(client valkey.Client) *Guarder {
	return &Guarder{client: client}
}

// Observe records one Sighting and returns a Verdict. It never mutates
// Discord state and never blocks on anything but Valkey.
func (g *Guarder) Observe(ctx context.Context, s Sighting) Verdict {
	link, invite := NormalizeLink(s.Link)
	if link == "" {
		return allow(ReasonBelowThreshold, link, invite)
	}
	if v, exempt := exemptVerdict(s, link, invite); exempt {
		return v
	}
	if v, hit := g.fleetHit(ctx, link, invite); hit {
		return v
	}
	return g.countAndDecide(ctx, s, link, invite)
}

// exemptVerdict applies the false-positive exemptions from the Sighting.
// Checked, and short-circuited, in this order: a guild's own invite is
// never actionable regardless of who posted it; a moderator posting is
// exempt next; the guild's configured allow-list last. None of these touch
// Valkey -- an exempt sighting leaves no trace, so a moderator's legitimate
// repeated posts never contribute to (or get blocked by) counts meant for
// abuse.
func exemptVerdict(s Sighting, link string, invite bool) (Verdict, bool) {
	switch {
	case s.OwnGuildInvite:
		return allow(ReasonOwnInvite, link, invite), true
	case s.Moderator:
		return allow(ReasonModerator, link, invite), true
	case s.Allowed:
		return allow(ReasonAllowListed, link, invite), true
	default:
		return Verdict{}, false
	}
}

// fleetHit checks whether link is already promoted fleet-wide. A promoted
// link is actioned in every guild that sees it, including one that has
// never independently tripped a local threshold -- that is the entire
// point of fleet promotion, and is safe specifically because promotion
// itself required FleetOwnerThreshold unrelated owners (see fleetPromote).
//
// A Valkey error here is treated as "not promoted" rather than propagated:
// failing to notice an existing promotion costs one sighting its fleet-wide
// fast path, not a false action, which matches this package's bias toward
// never over-blocking on infrastructure trouble. See ReasonValkeyError.
func (g *Guarder) fleetHit(ctx context.Context, link string, invite bool) (Verdict, bool) {
	raw, err := g.client.Do(ctx, g.client.B().Hget().Key(fleetKey(link)).Field(fleetFieldOwnerCount).Build()).ToString()
	if err != nil || raw == "" {
		return Verdict{}, false
	}
	count, _ := strconv.Atoi(raw)
	return Verdict{
		Allow:               false,
		Reason:              ReasonFleetPromoted,
		NormalizedLink:      link,
		IsInvite:            invite,
		FleetHit:            true,
		CorroboratingOwners: count,
	}, true
}

// countAndDecide records this sighting in the guild-local channel/author
// sets, reads their new cardinality, and decides whether a local threshold
// tripped. A tripped sighting goes on to check (and possibly perform) fleet
// promotion; a sighting that stays under threshold is allowed.
func (g *Guarder) countAndDecide(ctx context.Context, s Sighting, link string, invite bool) Verdict {
	channels, authors, err := g.recordAndCount(ctx, s, link)
	if err != nil {
		return allow(ReasonValkeyError, link, invite)
	}

	reason, tripped := tripReason(channels, authors)
	v := Verdict{
		Allow:            !tripped,
		Reason:           reason,
		NormalizedLink:   link,
		IsInvite:         invite,
		DistinctChannels: channels,
		DistinctAuthors:  authors,
		GuildTripped:     tripped,
	}
	if !tripped {
		return v
	}
	return g.corroborate(ctx, s.OwnerID, v)
}

// tripReason applies ChannelThreshold and AuthorThreshold. Channel first:
// it is the more common attack shape (one hijacked account, many channels),
// so checking it first keeps the common case a single comparison.
func tripReason(channels, authors int) (string, bool) {
	if channels >= ChannelThreshold {
		return ReasonChannelThreshold, true
	}
	if authors >= AuthorThreshold {
		return ReasonAuthorThreshold, true
	}
	return ReasonBelowThreshold, false
}

// recordAndCount adds ChannelID and UserID to this guild+link's sets and
// returns their new cardinalities. Each set's expiry is set with NX (only
// when the key has none yet), which is what gives Window a fixed start
// rather than one that resets on every new member -- see Window's doc.
func (g *Guarder) recordAndCount(ctx context.Context, s Sighting, link string) (channels, authors int, err error) {
	ck, ak := channelsKey(s.GuildID, link), authorsKey(s.GuildID, link)
	if err = g.addAndExpire(ctx, ck, s.ChannelID, Window); err != nil {
		return 0, 0, err
	}
	if err = g.addAndExpire(ctx, ak, s.UserID, Window); err != nil {
		return 0, 0, err
	}
	if channels, err = g.card(ctx, ck); err != nil {
		return 0, 0, err
	}
	authors, err = g.card(ctx, ak)
	return channels, authors, err
}

// corroborate records that ownerID independently tripped on link, then
// checks (and, if warranted, performs) fleet promotion.
//
// An empty ownerID (the guild has no Twitch broadcaster binding on record,
// i.e. it never completed setup) is deliberately NOT recorded here at all:
// the trips set is what FleetOwnerThreshold counts distinct entries from,
// and an unbound guild has no verified owner behind it. Letting it
// contribute would let an attacker skip setup entirely and reopen exactly
// the free-multiplication hole FleetOwnerThreshold exists to close (an
// unenrolled guild costs nothing either). This only withholds this guild's
// contribution to FLEET promotion -- the local verdict (v, already decided
// by countAndDecide) still stands, so detection and any per-guild
// enforcement in an unbound guild are unaffected.
func (g *Guarder) corroborate(ctx context.Context, ownerID string, v Verdict) Verdict {
	if ownerID == "" {
		return v
	}
	tk := tripsKey(v.NormalizedLink)
	if err := g.addAndExpire(ctx, tk, ownerID, CorroborationWindow); err != nil {
		return v
	}
	owners, err := g.card(ctx, tk)
	if err != nil {
		return v
	}
	v.CorroboratingOwners = owners
	if owners < FleetOwnerThreshold {
		return v
	}
	if err := g.fleetPromote(ctx, v.NormalizedLink, owners); err == nil {
		v.FleetPromoted = true
	}
	return v
}

// fleetPromote writes the fleet-wide "known bad" entry for link.
//
// This is deliberately the only place that ever writes a fleetKey, and it
// is only ever reached after corroborate has confirmed FleetOwnerThreshold
// distinct Twitch-broadcaster OWNERS independently tripped (see corroborate
// and the FleetOwnerThreshold doc for why that bar, and that unit, exist).
// The hash carries enough provenance for a human to audit and reverse the
// promotion by hand if it turns out to be wrong -- when, and how many
// distinct owners corroborated it -- rather than just a boolean flag that
// promoted a link for reasons nobody can later reconstruct. TTL is set
// every time this runs (not NX): a link that keeps earning fresh
// corroboration while already promoted should have its block extended, not
// silently expire out from under an active incident.
func (g *Guarder) fleetPromote(ctx context.Context, link string, ownerCount int) error {
	key := fleetKey(link)
	err := g.client.Do(ctx, g.client.B().Hset().Key(key).
		FieldValue().FieldValue(fleetFieldOwnerCount, strconv.Itoa(ownerCount)).
		FieldValue(fleetFieldPromotedAt, strconv.FormatInt(time.Now().Unix(), 10)).
		Build()).Error()
	if err != nil {
		return err
	}
	return g.client.Do(ctx, g.client.B().Expire().Key(key).Seconds(int64(FleetTTL.Seconds())).Build()).Error()
}

// addAndExpire adds member to the Valkey set at key and gives it ttl if (and
// only if) it does not already have an expiry -- see Window's doc for why
// NX rather than an unconditional refresh.
func (g *Guarder) addAndExpire(ctx context.Context, key, member string, ttl time.Duration) error {
	if err := g.client.Do(ctx, g.client.B().Sadd().Key(key).Member(member).Build()).Error(); err != nil {
		return err
	}
	return g.client.Do(ctx, g.client.B().Expire().Key(key).Seconds(int64(ttl.Seconds())).Nx().Build()).Error()
}

// card returns the cardinality of the Valkey set at key.
func (g *Guarder) card(ctx context.Context, key string) (int, error) {
	n, err := g.client.Do(ctx, g.client.B().Scard().Key(key).Build()).AsInt64()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// Key builders. All keys live under the "discord:linkguard:" prefix
// documented in the package doc; keeping the prefix and the separator here
// in one place is what keeps that doc comment accurate.
const keyPrefix = "discord:linkguard:"

const (
	fleetFieldOwnerCount = "owner_count"
	fleetFieldPromotedAt = "promoted_at"
)

func channelsKey(guildID, link string) string { return keyPrefix + "channels:" + guildID + ":" + link }
func authorsKey(guildID, link string) string  { return keyPrefix + "authors:" + guildID + ":" + link }
func tripsKey(link string) string             { return keyPrefix + "trips:" + link }
func fleetKey(link string) string             { return keyPrefix + "fleet:" + link }
