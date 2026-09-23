// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/pkg/tmpl"

	"go.uber.org/zap"
)

// timerRun is the single timer fire a chain plans for — the engine's mirror
// of commandRun, minus everything a tick has no chatter behind it to supply.
// ref is the (broadcaster, timer id) pair every log line here is keyed on;
// locale is read once by fire (which already reads the broadcaster row for
// the outgress lane) rather than costing timerChain a second projection
// read; firedAt is the tick's own instant, which timerRunIdentity turns into
// the {urlfetch} redelivery guard's claim key.
type timerRun struct {
	ref     timerRef
	locale  string
	firedAt time.Time
}

// timerRunIdentity stands in for a chat envelope's EventIdentity on the one
// scope that still needs a redelivery-guard identity: scope.External claims
// EventIdentity(&c.Env) per {urlfetch} name so a REDELIVERED chat line never
// burns the broadcaster's fetch quota twice. A tick has no envelope and no
// MsgID/EventID behind it — a Valkey key expiry fires it once, there is no
// consumer redelivering it — so leaving Env.MsgID empty would give every
// broadcaster's every tick the same identity (EventIdentity's own empty
// fallback), and the SECOND fetch anywhere would read as a replay of the
// first and skip its network call forever. Stamping one unique value per
// fire instead makes the guard a no-op for timers, which is the correct
// behavior (nothing here is ever redelivered) without leaving a shared claim
// path landmined for the next caller that builds a synthetic Context against
// it.
func timerRunIdentity(ref timerRef, firedAt time.Time) string {
	return "timer:" + strconv.FormatUint(ref.broadcasterID, 10) + ":" + ref.id + ":" + strconv.FormatInt(firedAt.UnixNano(), 10)
}

// timerChain builds the scope chain one timer fire expands its message
// through. It is commandChain's sibling, not a rewrite of it: same
// precedence, same mount-only-when-wired rule, but built for a tick instead
// of a chat line.
//
// A timer has no chatter, no command args and no message line behind it — it
// fires off a schedule key, not something a chatter typed — so every family
// commandChain mounts from the triggering line (scope.Message, scope.Uses)
// or from who is asking (scope.Viewer's {followage}/{accountage}/{points},
// scope.Store's {counter}) has nothing to read here. Leaving them out of the
// chain is what makes {user}, {touser}, {1}, {followage} and {counter:...}
// stay literal in a timer's own message: the same "no mounted scope owns
// this name" rule a typo gets, not a special case for timers.
//
// What stays is everything channel-scoped, already safe with nobody chatting:
// the dice (scope.Pure), the chat room (scope.Chatters, drawing off the same
// roster a command draws), the emote catalog (scope.Emotes), the channel
// facts (scope.Channel: {uptime}/{title}/{game}/{channel.viewers}), the
// module facts (scope.Modules: {quote}/{time}/{song}) and urlfetch
// (scope.External) — every one of these already answers a custom command
// with no chatter identity in its own tokens, so a timer mounts them exactly
// the way commandChain does, reusing the same per-scope mount helpers
// (chattersScope, emotesScope, channelScope, moduleScope) so a broadcaster's
// module toggle can never gate a command's token one way and a timer's the
// other.
func (p *Pipeline) timerChain(ctx context.Context, run timerRun, toks []tmpl.Token) scope.Chain {
	c := &module.Context{
		BroadcasterID: run.ref.broadcasterID,
		Locale:        run.locale,
		Env: lane.Envelope{
			BroadcasterUserID: strconv.FormatUint(run.ref.broadcasterID, 10),
			MsgID:             timerRunIdentity(run.ref, run.firedAt),
		},
	}
	chain := scope.Chain{
		scope.Pure{Locale: run.locale},
		p.chattersScope(c, toks),
	}
	if emotes, mounted := p.emotesScope(toks); mounted {
		chain = append(chain, emotes)
	}
	if channel, mounted := p.channelScope(ctx, c, toks); mounted {
		chain = append(chain, channel)
	}
	if mods, mounted := p.moduleScope(ctx, c, toks); mounted {
		chain = append(chain, mods)
	}
	// Max stays maxUrlFetchTokens (8), the same engine-side backstop a
	// command's External mount uses — it exists for a legacy or corrupt row
	// that predates or bypasses save-time validation, never as the normal
	// budget. The normal budget is enforced at SAVE time instead
	// (web/dashboard/src/lib/server/timers-parse.ts), at URLFETCH_TOKEN_CAP
	// (3), the same cap a command response gets: gossip's custom.fetch is
	// rate-limited to 6/min PER CHANNEL (app/gossip/internal/providers/
	// custom/custom.go's ChannelRateLimit), and a timer's own floor is one
	// fire per 60s, so one timer alone naming 3 distinct defs spends at most
	// half that channel's whole budget every fire, leaving room for a custom
	// command run the same minute. A row that slipped past validation (an
	// old save, a direct write) is still capped at 8 by Max here, same as a
	// command's — the fan-out backstop was never meant to be reachable from
	// the save path, on either surface.
	if p.customFetch != nil {
		chain = append(chain, scope.External{
			Fetcher: urlFetches{p: p, run: commandRun{c: c, command: "timer:" + run.ref.id}},
			Max:     maxUrlFetchTokens,
		})
	}
	return chain
}

// logTimerScopeFailure is timerChain's mirror of logScopeFailure: a scope
// whose Plan fails degrades to that scope's own tokens rendering empty (or
// their fallback), never a dropped timer — logged against the timer ref
// rather than a module.Context, because a tick has no chat envelope for
// module.BIDField's usual caller to read.
func (p *Pipeline) logTimerScopeFailure(ref timerRef) func(error) {
	return func(err error) {
		p.log.Warn("timer scope plan failed", module.BIDField(ref.broadcasterID), zap.String("timer_id", ref.id), zap.Error(err))
	}
}

// expandTimerText runs one timer's stored message through the same
// Lex -> hasVarToken -> Plan -> Render sequence expandCommandText runs for a
// custom command, against timerChain instead of commandChain. A message
// naming no token is returned unchanged, byte-identical, at the cost of one
// Lex: the Plan/Render round trip only runs for a message that actually
// spends it, the same guarantee commandChain's own per-scope wants give a
// command.
func (p *Pipeline) expandTimerText(ctx context.Context, run timerRun, text string) string {
	toks := tmpl.Lex(text)
	if !hasVarToken(toks) {
		return text
	}
	chain := p.timerChain(ctx, run, tmpl.WithCondRefs(toks))
	values := chain.Plan(ctx, toks, p.logTimerScopeFailure(run.ref))
	buf := GetBuf()
	buf = chain.Render(buf, toks, values)
	out := string(buf)
	PutBuf(buf)
	return out
}

// defangTimerSlashLines strips a leading run of '/' or ' ' off EVERY line of
// a timer's stored message, before expansion ever sees it.
//
// It exists because chatLines' Translate call (timerOutputs, timers_valkey.go)
// now runs over every published line whenever a pipeline is wired, token or
// not — where fire used to post raw with no slash-verb routing at all. A
// timer was never a place a broadcaster could route a slash-verb on purpose
// (unlike a custom command's response, which is): a message saved back when
// fire posted raw — "/me does a little dance" as flavor text on its own
// line, say — must not gain the power to ban, timeout or announce just
// because this PR taught the pipe to expand tokens. Every line is checked,
// not only the first, because chatLines translates each one independently.
//
// This is deliberately NOT vars.go's sanitizeVar, despite solving the same
// class of problem: sanitizeVar's stripControls half removes every embedded
// control rune in the whole string, newlines included, which is exactly
// right for a single user-supplied VALUE ({touser}, {args}) and exactly
// wrong for a whole multi-line TEMPLATE — it would silently delete every
// newline a broadcaster wrote on purpose, breaking the very multi-line
// fan-out fire now provides via chatLines. Confirmed empirically, not
// assumed: sanitizeVar("line1\nline2") returns "line1line2".
func defangTimerSlashLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = defangLeadingSlash(line)
	}
	return strings.Join(lines, "\n")
}

// defangLeadingSlash trims a leading run of '/' or ' ' from one line — the
// same rule vars.go's trimLeftSlashSpace applies, reimplemented rather than
// called because that helper is gated behind sanitizeVar's rawText type
// (stripControls has to run first, by that file's own decision record), and
// running stripControls first is exactly the behavior defangTimerSlashLines'
// own comment rules out.
func defangLeadingSlash(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '/') {
		i++
	}
	return s[i:]
}
