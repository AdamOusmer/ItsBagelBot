// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkguard

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

const (
	ChannelThreshold = 3

	AuthorThreshold = 2

	Window = 10 * time.Minute

	FleetOwnerThreshold = 2

	FleetTTL = 7 * 24 * time.Hour

	CorroborationWindow = 24 * time.Hour
)

const (
	ReasonOwnInvite        = "own_invite"
	ReasonModerator        = "moderator"
	ReasonAllowListed      = "allow_listed"
	ReasonFleetPromoted    = "fleet_promoted"
	ReasonChannelThreshold = "channel_threshold"
	ReasonAuthorThreshold  = "author_threshold"
	ReasonBelowThreshold   = "below_threshold"
	ReasonValkeyError      = "valkey_error"
)

type Sighting struct {
	GuildID   string
	ChannelID string
	UserID    string
	MessageID string
	Link      string

	OwnerID string

	OwnGuildInvite bool

	Moderator bool

	Allowed bool
}

type Verdict struct {
	Allow  bool
	Reason string

	NormalizedLink string
	IsInvite       bool

	DistinctChannels int
	DistinctAuthors  int

	GuildTripped        bool
	FleetHit            bool
	FleetPromoted       bool
	CorroboratingOwners int
}

type normalizedLink string

type valkeyKey string

func allow(reason string, link normalizedLink, invite bool) Verdict {
	return Verdict{Allow: true, Reason: reason, NormalizedLink: string(link), IsInvite: invite}
}

type Guarder struct {
	client valkey.Client
}

func New(client valkey.Client) *Guarder {
	return &Guarder{client: client}
}

func (g *Guarder) Observe(ctx context.Context, s Sighting) Verdict {
	raw, invite := NormalizeLink(s.Link)
	link := normalizedLink(raw)
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

func exemptVerdict(s Sighting, link normalizedLink, invite bool) (Verdict, bool) {
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

func (g *Guarder) fleetHit(ctx context.Context, link normalizedLink, invite bool) (Verdict, bool) {
	raw, err := g.client.Do(ctx, g.client.B().Hget().Key(string(fleetKey(link))).Field(fleetFieldOwnerCount).Build()).ToString()
	if err != nil || raw == "" {
		return Verdict{}, false
	}
	count, _ := strconv.Atoi(raw)
	return Verdict{
		Allow:               false,
		Reason:              ReasonFleetPromoted,
		NormalizedLink:      string(link),
		IsInvite:            invite,
		FleetHit:            true,
		CorroboratingOwners: count,
	}, true
}

func (g *Guarder) countAndDecide(ctx context.Context, s Sighting, link normalizedLink, invite bool) Verdict {
	channels, authors, err := g.recordAndCount(ctx, s, link)
	if err != nil {
		return allow(ReasonValkeyError, link, invite)
	}

	reason, tripped := tripReason(channels, authors)
	v := Verdict{
		Allow:            !tripped,
		Reason:           reason,
		NormalizedLink:   string(link),
		IsInvite:         invite,
		DistinctChannels: channels,
		DistinctAuthors:  authors,
		GuildTripped:     tripped,
	}
	if !tripped {
		return v
	}
	return g.corroborate(ctx, s, v)
}

func tripReason(channels, authors int) (string, bool) {
	if channels >= ChannelThreshold {
		return ReasonChannelThreshold, true
	}
	if authors >= AuthorThreshold {
		return ReasonAuthorThreshold, true
	}
	return ReasonBelowThreshold, false
}

func (g *Guarder) recordAndCount(ctx context.Context, s Sighting, link normalizedLink) (channels, authors int, err error) {
	gl := guildLink{GuildID: s.GuildID, Link: string(link)}
	ck, ak := gl.channelsKey(), gl.authorsKey()
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

func (g *Guarder) corroborate(ctx context.Context, s Sighting, v Verdict) Verdict {
	if s.OwnerID == "" {
		return v
	}
	tk := tripsKey(normalizedLink(v.NormalizedLink))
	if err := g.addAndExpire(ctx, tk, s.OwnerID, CorroborationWindow); err != nil {
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
	if err := g.fleetPromote(ctx, normalizedLink(v.NormalizedLink), owners); err == nil {
		v.FleetPromoted = true
	}
	return v
}

func (g *Guarder) fleetPromote(ctx context.Context, link normalizedLink, ownerCount int) error {
	key := fleetKey(link)
	err := g.client.Do(ctx, g.client.B().Hset().Key(string(key)).
		FieldValue().FieldValue(fleetFieldOwnerCount, strconv.Itoa(ownerCount)).
		FieldValue(fleetFieldPromotedAt, strconv.FormatInt(time.Now().Unix(), 10)).
		Build()).Error()
	if err != nil {
		return err
	}
	return g.client.Do(ctx, g.client.B().Expire().Key(string(key)).Seconds(int64(FleetTTL.Seconds())).Build()).Error()
}

func (g *Guarder) addAndExpire(ctx context.Context, key valkeyKey, member string, ttl time.Duration) error {
	if err := g.client.Do(ctx, g.client.B().Sadd().Key(string(key)).Member(member).Build()).Error(); err != nil {
		return err
	}
	return g.client.Do(ctx, g.client.B().Expire().Key(string(key)).Seconds(int64(ttl.Seconds())).Nx().Build()).Error()
}

func (g *Guarder) card(ctx context.Context, key valkeyKey) (int, error) {
	n, err := g.client.Do(ctx, g.client.B().Scard().Key(string(key)).Build()).AsInt64()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

const keyPrefix = "discord:linkguard:"

const (
	fleetFieldOwnerCount = "owner_count"
	fleetFieldPromotedAt = "promoted_at"
)

type guildLink struct {
	GuildID string
	Link    string
}

func (gl guildLink) channelsKey() valkeyKey {
	return valkeyKey(keyPrefix + "channels:" + gl.GuildID + ":" + gl.Link)
}

func (gl guildLink) authorsKey() valkeyKey {
	return valkeyKey(keyPrefix + "authors:" + gl.GuildID + ":" + gl.Link)
}

func tripsKey(link normalizedLink) valkeyKey { return valkeyKey(keyPrefix + "trips:" + string(link)) }
func fleetKey(link normalizedLink) valkeyKey { return valkeyKey(keyPrefix + "fleet:" + string(link)) }
