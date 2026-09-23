// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/tmpl"
)

// chattersScope builds the {chatters} / {random.chatter} scope for one command
// run.
//
// Unlike the viewer and module scopes this one is mounted UNCONDITIONALLY, and
// returns no "mounted" flag to decide on: there is no opt-in module behind it
// and no projection row to read, because the roster it answers from is fed by
// the very chat lines that trigger commands. Mounting costs nothing when the
// template names neither token — the chain only calls Plan for a scope some
// span actually wants, so the roster is not even read.
//
// toks is the lexed template because the bare {random.chatter} spans have to
// be COUNTED here: each is an independent draw, and the chain hands a scope
// distinct spans.
func (p *Pipeline) chattersScope(c *module.Context, toks []tmpl.Token) scope.Chatters {
	return scope.Chatters{
		Roster:  rosterView{roster: p.roster, broadcasterID: c.BroadcasterID},
		Exclude: p.undrawableChatters(c),
		Draws:   chatterDrawsOf(toks),

		Viewers:       p.chatterViewers(c),
		ViewerExclude: p.undrawableViewers(c),
		ViewerDraws:   viewerDrawsOf(toks),
	}
}

// chatterViewers builds {random.viewer}'s draw source. p.viewers is nil for
// a deployment that never wired it (Deps.Viewers); scope.Chatters mounts
// unconditionally either way, so a nil Viewers renders the draw empty (its
// fallback fires) rather than leaving the span literal — unlike a wired one,
// which never renders empty at all once mounted (see viewerSource).
func (p *Pipeline) chatterViewers(c *module.Context) scope.Viewers {
	if p.viewers == nil {
		return nil
	}
	return viewerSource{rpc: p.viewers, roster: p.roster, broadcasterID: c.BroadcasterID}
}

// undrawableChatters are the two identities {random.chatter} never names: the
// bot itself and the broadcaster.
//
// The bot is excluded belt-and-braces — the pipeline already drops its own
// echo before the roster observes a line (isOwnChat), so it should never be in
// there — because the two guards are configured by the same id and a channel
// where that id is unset must not start drawing the bot's own name. An unset
// id parses to zero, which the roster never stores, so the exclusion is inert
// rather than wrong.
//
// The broadcaster is excluded because the tokens exist to point at the room:
// "@{random.chatter} what do you think" addressed to the streamer reads as the
// bot talking to the person operating it.
func (p *Pipeline) undrawableChatters(c *module.Context) []uint64 {
	botID, _ := strconv.ParseUint(p.botID, 10, 64)
	return []uint64{botID, c.BroadcasterID}
}

// undrawableViewers is undrawableChatters plus the sender: the decision
// record for {random.viewer} is that a viewer running "!hug" must never hug
// themselves. {random.chatter} carries no such rule (see undrawableChatters),
// so this is {random.viewer}'s own exclusion set rather than a shared one.
func (p *Pipeline) undrawableViewers(c *module.Context) []uint64 {
	exclude := p.undrawableChatters(c)
	senderID, err := strconv.ParseUint(c.Env.ChatterUserID, 10, 64)
	if err == nil && senderID != 0 {
		exclude = append(exclude, senderID)
	}
	return exclude
}

// chatterDrawsOf and viewerDrawsOf count a template's bare {random.chatter}/
// {random.viewer} spans. A span carrying a payload is not one of them: it
// names nothing this token reads and stays literal, so counting it would pay
// for a draw nobody renders.
func chatterDrawsOf(toks []tmpl.Token) int { return bareDrawsOf(toks, scope.RandomChatterToken) }
func viewerDrawsOf(toks []tmpl.Token) int  { return bareDrawsOf(toks, scope.RandomViewerToken) }

func bareDrawsOf(toks []tmpl.Token, name string) int {
	draws := 0
	for _, tok := range toks {
		if isBareVar(tok, name) {
			draws++
		}
	}
	return draws
}

// isBareVar reports whether tok is a payload-free {name} span — the only
// shape that draws for chatterDrawsOf/viewerDrawsOf (see bareDrawsOf).
func isBareVar(tok tmpl.Token, name string) bool {
	return tok.Kind == tmpl.KindVar && tok.Name == name && !tok.HasPayload
}

// rosterView is the engine half of the chatter scope: it narrows the whole
// roster to the one channel this run belongs to and renders each entry's
// display name.
type rosterView struct {
	roster        *chatterRoster
	broadcasterID uint64
}

func (v rosterView) Chatters() []scope.Chatter {
	seen := v.roster.Snapshot(v.broadcasterID)
	out := make([]scope.Chatter, 0, len(seen))
	for i := range seen {
		out = append(out, scope.Chatter{ID: seen[i].ID, Name: chatterName(seen[i])})
	}
	return out
}

// viewerSource is the engine half of {random.viewer}: rpc answers "who does
// Twitch say is in chat right now", from the shared Valkey snapshot.
//
// Decision record: a cold snapshot and a latched MissingScope both degrade to
// the very roster {random.chatter} already draws from, rather than an empty
// draw. A hole in the reply (an unresolved "{random.viewer}" or a silent
// fallback) is worse than naming someone who spoke recently instead of
// someone who is merely present — and the roster-backed answer is only ever
// needed for a channel the loyalty tick has not warmed yet (not live, or
// newly live) or one it cannot read at all; a live channel's snapshot is kept
// warm by that same tick (see chattersSnapshotTTL), so the viewer-list
// guarantee holds whenever it matters most.
type viewerSource struct {
	rpc           ViewerLookup
	roster        *chatterRoster
	broadcasterID uint64
}

func (v viewerSource) Viewers(ctx context.Context) ([]scope.Chatter, bool) {
	entries, state := v.rpc.Snapshot(ctx, v.broadcasterID)
	if state == viewerSnapshotOK {
		return viewerChatters(entries), true
	}
	return rosterView{roster: v.roster, broadcasterID: v.broadcasterID}.Chatters(), true
}

// viewerChatters renders each cached entry the same way a roster draw does
// (chatterName's Name-falling-back-to-Login shape): it arrived on the chat
// wire (Helix relays whatever login the viewer carries), and a drawn name
// goes straight into a broadcaster's chat line, so it is sanitized the same
// way for the same reason.
func viewerChatters(entries []chattersSnapshotEntry) []scope.Chatter {
	out := make([]scope.Chatter, 0, len(entries))
	for _, e := range entries {
		out = append(out, scope.Chatter{ID: e.ID, Name: chatterName(Viewer{ID: e.ID, Login: e.Login, Name: e.Name})})
	}
	return out
}

// chatterName is what a drawn chatter renders as: their display name, falling
// back to the login when the roster only ever learned that (a folded-cohort
// sender carries no display name on the wire).
//
// It goes through sanitizeVar for the reason {touser} does, and it is the same
// threat: every byte of a roster name arrived on the chat wire, chosen by the
// viewer it names. An unsanitized draw would let a viewer who sets their
// display name to a leading slash-verb have the bot say it, through a
// broadcaster's template that never mentioned them.
func chatterName(who Viewer) string {
	name := who.Name
	if name == "" {
		name = who.Login
	}
	return sanitizeVar(name)
}
