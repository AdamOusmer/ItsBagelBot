// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"regexp"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
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
func LinkGuard(guard Guarder, log *zap.Logger) module.Module {
	h := linkGuardModule{guard: guard, log: log}
	b := module.NewModule("linkguard")
	b.On("MESSAGE_CREATE", h.onCreate)
	return b.Build()
}

type linkGuardModule struct {
	guard Guarder
	log   *zap.Logger
}

// onCreate builds one linkguard.Sighting per link the message contains and
// acts on at most one non-Allow Verdict (see observeLinks and act). The
// bot's own messages, and every other bot's, are excluded first and
// unconditionally -- linkguard has no notion of "author is a bot", and
// this engine must never delete or count its own posts (a go-live embed,
// a log line) or another automod bot's.
func (h linkGuardModule) onCreate(ctx context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.MessageEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if ev.Author.Bot || ev.GuildID == "" || !c.Config.LinkGuardOn() {
		return nil
	}
	links := linkPattern.FindAllString(ev.Content, -1)
	if len(links) == 0 {
		return nil
	}
	moderator := decode.HasRole(ev.Member.Roles, c.Config.ModsRoleID)
	if reason := h.observeLinks(ctx, c, ev, links, moderator); reason != "" {
		h.act(c, emit, ev, reason)
	}
	return nil
}

// observeLinks reports every link in links to guard.Observe and returns
// the first tripped Verdict's Reason, or "" when every link was allowed.
// Every link is observed, not just the first -- linkguard's counters are
// per (guild, link), so a message's second or third link still needs its
// own count recorded even after an earlier link in the same message
// already tripped -- but the return value collapses to a single reason,
// which is what lets onCreate emit at most one delete for the message
// regardless of how many links it carried or how many of them tripped.
// Identical links within one message (normalized) are only sent to
// guard.Observe once: recording the same channel+link pair a second time
// would be a no-op on linkguard's side anyway (its sets are keyed on
// membership, see guard.go's addAndExpire), so skipping the repeat here
// just saves the redundant Valkey round trip.
func (h linkGuardModule) observeLinks(ctx context.Context, c *module.Context, ev decode.MessageEvent, links []string, moderator bool) string {
	reason := ""
	seen := make(map[string]bool, len(links))
	for _, raw := range links {
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
			OwnerID: c.BroadcasterID,
			// OwnGuildInvite is always false: telling "this invite points
			// back to GuildID itself" from "some other server's invite"
			// needs either a Discord API call (GET /invites/{code}) or a
			// locally maintained cache of invite codes this guild is
			// known to have issued, and this codebase has neither --
			// engine deliberately holds no Discord REST client (see
			// app/discord/engine's package doc), and nothing in
			// discordstore, discordapi, or the outgress setup fill
			// tracks a guild's invite codes today (the bot never creates
			// one). Building either would be new infrastructure well
			// beyond wiring linkguard in. The practical consequence is
			// narrow: a guild that legitimately cross-posts its own
			// invite in several channels needs that invite on
			// LinkAllowList (Config.LinkAllowed) to avoid tripping
			// ChannelThreshold, which is a one-time admin action with the
			// same end result (Allow) as this field would have given it,
			// just under ReasonAllowListed instead of ReasonOwnInvite.
			OwnGuildInvite: false,
			Moderator:      moderator,
			Allowed:        c.Config.LinkAllowed(raw),
		})
		if !v.Allow && reason == "" {
			reason = v.Reason
		}
	}
	return reason
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
	emit(cmd.DeleteMessage(c.Config.GuildID, ev.ChannelID, ev.ID, "linkguard: "+reason))
	body := decode.Mention(ev.Author) + " in <#" + ev.ChannelID + "> (" + reason + ")"
	_ = logLine(c, emit, logEntry{Title: "Link removed", Body: body})
}
