// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
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
	}
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

// chatterDrawsOf counts the template's bare {random.chatter} spans. A span
// carrying a payload is not one of them: it names nothing this token reads and
// stays literal, so counting it would pay for a draw nobody renders.
func chatterDrawsOf(toks []tmpl.Token) int {
	draws := 0
	for _, tok := range toks {
		if isBareRandomChatter(tok) {
			draws++
		}
	}
	return draws
}

func isBareRandomChatter(tok tmpl.Token) bool {
	return tok.Kind == tmpl.KindVar && tok.Name == scope.RandomChatterToken && !tok.HasPayload
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
