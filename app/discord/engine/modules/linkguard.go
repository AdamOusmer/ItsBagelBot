// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"regexp"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/discord/linkguard"

	"go.uber.org/zap"
)

// Guarder is the subset of *linkguard.Guarder this module calls. Accepting
// the interface rather than the concrete type lets tests substitute a fake
// with no Valkey round trip at all (see linkguard_test.go), the same
// purgeClient/voiceClient shape moderation.go and voice.go already use for
// their own RPC collaborators.
//
// Observe never returns an error -- a Valkey problem is folded into the
// Verdict itself (Allow: true, Reason: linkguard.ReasonValkeyError; see
// guard.go's countAndDecide/fleetHit). That is deliberate on linkguard's
// side: a broker blip must never itself start deleting messages, only cost
// this one sighting its detection. Because that fail-open behavior already
// lives in the Verdict, this module needs no special-case error handling
// of its own -- v.Allow is already true, so act (below) is simply never
// called.
type Guarder interface {
	Observe(ctx context.Context, s linkguard.Sighting) linkguard.Verdict
}

// linkPattern finds domain-shaped candidate links anywhere in a message: a
// run of letters/digits/dots/hyphens ending in a literal dot plus a 2+
// letter TLD, with an optional path. This is only a candidate filter --
// linkguard.NormalizeLink (via guard.Observe) does the real parsing,
// including stripping a scheme this pattern does not require. It is
// deliberately schemeless-friendly, since "discord.gg/x" with no
// "https://" is how these overwhelmingly get pasted, and deliberately
// narrow enough that ordinary prose never matches: "e.g.", "Mr. Smith",
// "3.14" and "U.S." all fail because none of them has 2+ letters
// immediately following a dot with no space in between, which
// `[a-z]{2,}` requires.
var linkPattern = regexp.MustCompile(`(?i)[a-z0-9][a-z0-9.-]*\.[a-z]{2,}(?:/\S*)?`)

// LinkGuard wires internal/domain/discord/linkguard into MESSAGE_CREATE:
// see that package's doc for the attack it catches (a hijacked account, or
// a small batch of accounts, spraying an NSFW/scam invite across a
// guild's channels, and that same link then reappearing across
// independently-owned guilds in the fleet) and why deletion -- not a ban
// or kick -- is the automod response (see act's doc). Gated on
// Config.LinkGuardOn so a guild can turn it off, the same way every other
// Discord feature here reads its own cfg.XOn() flag.
//
// own is consulted only after a Verdict has already tripped and only for a
// link NormalizeLink tags as a Discord invite -- see tripIsOwnInvite's doc
// for exactly when and why, and OwnInviteChecker's doc in
// linkguard_invite.go for the cost this guards against.
func LinkGuard(guard Guarder, own OwnInviteChecker, log *zap.Logger) module.Module {
	h := linkGuardModule{guard: guard, own: own, log: log}
	b := module.NewModule("linkguard")
	b.On("MESSAGE_CREATE", h.onCreate)
	return b.Build()
}

type linkGuardModule struct {
	guard Guarder
	own   OwnInviteChecker
	log   *zap.Logger
}

// onCreate builds one linkguard.Sighting per link the message contains and
// acts on at most one non-Allow Verdict (see observeLinks and act). See
// skipMessage for what gets ignored before any of that runs.
func (h linkGuardModule) onCreate(ctx context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.MessageEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if skipMessage(ev, c.Config) {
		return nil
	}
	links := linkPattern.FindAllString(ev.Content, -1)
	if len(links) == 0 {
		return nil
	}
	moderator := decode.HasAnyRole(ev.Member.Roles, c.Config.StaffRoleIDs())
	in := linkObservation{Module: c, Event: ev, Links: links, Moderator: moderator}
	if reason := h.observeLinks(ctx, in); reason != "" {
		h.act(c, emit, ev, reason)
	}
	return nil
}

// linkObservation is observeLinks's whole input beyond ctx, which Go
// convention keeps as its own leading parameter rather than folding into a
// struct. Collapsed from four separate parameters (module.Context, the
// decoded event, the link list, and the moderator flag) into one struct:
// CodeScene's Excess Number of Function Arguments flagged observeLinks at
// five parameters (including the receiver and ctx), over its 4-parameter
// limit.
type linkObservation struct {
	Module    *module.Context
	Event     decode.MessageEvent
	Links     []string
	Moderator bool
}

// skipMessage reports whether onCreate should ignore ev entirely: the bot's
// own messages, and every other bot's, are excluded first and
// unconditionally -- linkguard has no notion of "author is a bot", and this
// engine must never delete or count its own posts (a go-live embed, a log
// line) or another automod bot's; a message with no guild id (e.g. a DM)
// has nothing for LinkGuardOn to gate; and a guild with the module off is
// not watched at all.
func skipMessage(ev decode.MessageEvent, cfg ddiscord.Config) bool {
	return ev.Author.Bot || ev.GuildID == "" || !cfg.LinkGuardOn()
}

// observeLinks reports every link in links to guard.Observe and returns
// the first tripped, still-actionable Verdict's Reason, or "" when every
// link was allowed, exempted, or (via tripIsOwnInvite) resolved to be the
// posting guild's own invite. Every link is observed, not just the first
// -- linkguard's counters are per (guild, link), so a message's second or
// third link still needs its own count recorded even after an earlier link
// in the same message already tripped -- but the return value collapses to
// a single reason, which is what lets onCreate emit at most one delete for
// the message regardless of how many links it carried or how many of them
// tripped. Identical links within one message (normalized) are only sent
// to guard.Observe once: recording the same channel+link pair a second
// time would be a no-op on linkguard's side anyway (its sets are keyed on
// membership, see guard.go's addAndExpire), so skipping the repeat here
// just saves the redundant Valkey round trip.
//
// Sighting.OwnGuildInvite is always false on this call, unconditionally --
// see tripIsOwnInvite's doc for why that check happens AFTER Observe
// instead of before, and what it costs to run it that way.
func (h linkGuardModule) observeLinks(ctx context.Context, in linkObservation) string {
	ev, c := in.Event, in.Module
	reason := ""
	seen := make(map[string]bool, len(in.Links))
	for _, raw := range in.Links {
		norm, _ := linkguard.NormalizeLink(raw)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		v := h.guard.Observe(ctx, linkguard.Sighting{
			GuildID:   ev.GuildID,
			ChannelID: ev.ChannelID,
			UserID:    ev.Author.ID,
			MessageID: ev.ID,
			Link:      raw,
			// OwnerID: the Twitch broadcaster GuildID is bound to. Reused
			// directly from the dispatcher's own resolve.Resolver.ByGuild
			// result (module.Context.BroadcasterID) rather than a second
			// lookup against the discord:guild:{id} reverse index --
			// dispatch.go already resolved it before any Handler ran, and
			// a Handler only ever runs for a guild that resolution
			// succeeded for.
			OwnerID:   c.BroadcasterID,
			Moderator: in.Moderator,
			Allowed:   c.Config.LinkAllowed(raw),
		})
		if v.Allow || reason != "" {
			continue
		}
		if h.tripIsOwnInvite(ctx, ev.GuildID, raw, v) {
			continue
		}
		reason = v.Reason
	}
	return reason
}

// tripIsOwnInvite is the Discord-aware half of the OwnGuildInvite exemption
// that internal/domain/discord/linkguard's Sighting doc explicitly leaves
// to the caller (see that package's "Scope" doc: it makes no Discord API
// calls of its own). It runs ONLY for v, a Verdict that ALREADY tripped a
// threshold (Observe was called with OwnGuildInvite always false -- see
// observeLinks), and only when v.IsInvite -- a non-invite URL has no guild
// for it to resolve to, and an already-Allowed verdict needs no resolving
// at all.
//
// That ordering -- resolve after the trip, never before -- is deliberate,
// for two reasons:
//
//  1. Cost. Resolving on every posted invite link (rather than only a
//     tripped one) turns a spam wave of hundreds of junk codes into an
//     equal-sized wave of outgress REST calls against the shared ~50 req/s
//     bot-token budget -- exactly the amplification LinkGuard's own doc
//     warns about. A trip is rare (ChannelThreshold needs 3 distinct
//     channels inside one Window); a message is not. Gating resolution on
//     "already tripped" is what keeps this module's Discord-call rate tied
//     to actual suspicious activity instead of ordinary chat volume.
//  2. Package boundary. Resolving before Observe would need a second,
//     Valkey-free "would this trip" peek added to linkguard.Guarder next
//     to Observe, purely so this one caller could decide whether resolving
//     is worth it -- see linkguard's own package doc, "Scope", for why it
//     stays a pure function of a Sighting plus Valkey state and nothing
//     else. Resolving after keeps Observe linkguard's only entry point.
//
// What ordering it this way costs: a guild's own invite, pinned across
// enough channels to trip ChannelThreshold, is still recorded in
// linkguard's channel/author sets (and, if this guild has a Twitch-
// broadcaster binding, still contributes one trip toward fleet
// corroboration) even though no action follows -- the sighting is counted,
// just not acted on. That is judged acceptable, not a correctness bug,
// because a Discord invite code is unique to the guild it was created for:
// this guild's own pinned code can only ever inflate ITS OWN counters for
// THAT ONE CODE, and can never collide with, or poison, any other guild's
// count. Fleet promotion of that code still requires FleetOwnerThreshold
// (2) DISTINCT owners independently tripping on it (see
// linkguard.FleetOwnerThreshold's doc); this guild alone can only ever
// supply one. A second owner tripping on the SAME code is, by
// construction, some other guild spamming or cross-posting THIS guild's
// invite -- exactly the cross-guild pattern fleet promotion exists to
// catch, regardless of whose invite it targets. Resolving BEFORE Observe
// instead, so an own-invite sighting were never counted at all, was
// rejected because it cannot be made lazy the way point 1 requires:
// "should I even bother resolving" is not answerable until Observe has
// already said whether this sighting matters.
//
// On an unresolvable answer (IsOwnGuildInvite's RPC, or the Discord call
// behind it, failed), this treats the link as the guild's OWN invite --
// i.e. it skips the delete -- rather than proceeding with it. That is the
// deliberate choice, not the default one: a false delete during a Discord/
// outgress hiccup is visible and damaging to a real community (their own
// pinned invite starts vanishing from #rules for a reason no moderator can
// see), while a missed spam message during that same hiccup is one
// message, and ChannelThreshold's own design means a genuine attack keeps
// spraying the same link into more channels -- the very next sighting gets
// a fresh chance to resolve and act once the outage clears. Erring toward
// the reversible cost (a delayed action) over the visible one (an
// incorrect action against innocent behavior) matches act's own doc for
// why deletion, never a ban, is this module's ceiling in the first place.
func (h linkGuardModule) tripIsOwnInvite(ctx context.Context, guildID, raw string, v linkguard.Verdict) bool {
	if !v.IsInvite || h.own == nil {
		return false
	}
	own, err := h.own.IsOwnGuildInvite(ctx, guildID, raw)
	if err != nil {
		h.log.Warn("linkguard: invite resolve failed, not deleting", zap.String("guild_id", guildID), zap.Error(err))
		return true
	}
	return own
}

// act deletes the offending message and logs why. Deletion, never a ban or
// kick, is the automod action here: it is the only response that scales to
// a spam wave without exhausting Discord's roughly 50 requests/second
// per-bot-token budget (banning every account in a multi-account wave
// competes with every other guild's traffic on that same shared budget),
// and it is not destructive if the detection turns out to be wrong -- the
// author can still post, still be warned, still appeal, none of which is
// true of a ban. Escalating beyond deletion is a later, separate decision
// this module deliberately does not make.
func (h linkGuardModule) act(c *module.Context, emit module.Emit, ev decode.MessageEvent, reason string) {
	emit(cmd.DeleteMessage(cmd.ChannelTarget(c.Config.GuildID, ev.ChannelID), ev.ID, cmd.Reason("linkguard: "+reason)))
	body := decode.Mention(ev.Author) + " in <#" + ev.ChannelID + "> (" + reason + ")"
	_ = logLine(c, emit, logEntry{Title: "Link removed", Body: body})
}
