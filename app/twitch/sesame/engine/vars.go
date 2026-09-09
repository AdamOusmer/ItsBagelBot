// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"

	"go.uber.org/zap"
)

// commandChain builds the scope chain one custom-command run expands through.
//
// It replaced expandCommand's switch: a token family is no longer an arm
// nothing can gate, it is a scope that is present or absent, so "this module
// is off for this broadcaster" and "this dependency is not wired" are the same
// statement and both leave the token literal. Order is precedence — pure
// first, so a broadcaster cannot shadow {random} — and external last, because
// its Plan is the only one that leaves the process.
//
// args is the RAW argument string: the counter scope resolves a mention from
// it, and that resolution has to see the same bytes the chatter typed.
func (p *Pipeline) commandChain(run commandRun) scope.Chain {
	chain := scope.Chain{scope.Pure{}, messageVars(run)}
	if p.loyalty != nil {
		chain = append(chain, scope.Store{Counters: newCounterBumps(p, run)})
	}
	if p.customFetch != nil {
		chain = append(chain, scope.External{
			Fetcher: urlFetches{p: p, run: run},
			Max:     maxUrlFetchTokens,
		})
	}
	return chain
}

// commandRun is the single custom-command run a chain plans for: the module
// context it fires in, the canonical command name, and the RAW argument
// string the chatter typed.
//
// The three travel together through every scope the chain mounts — the
// message tokens, the counter bumps and the url fetches each need all three —
// so they are one value rather than three parameters rethreaded at each hop,
// which is what let a caller pass them in the wrong order.
type commandRun struct {
	c       *module.Context
	command string
	args    string
}

// messageVars reads the triggering chat line's identity tokens.
//
// The user-controlled halves ({args}, {touser}) are run through sanitizeVar
// here, at the one boundary that mints them, so a crafted argument can never
// inject a leading slash-verb (/ban, /timeout) into the expanded response for
// Translate to route. Command CONTENT is validated at save time on the
// dashboard; this guards only the runtime injection vector. The '@' is
// trimmed after sanitizing as well as before, so "@@bob" still renders "bob".
func messageVars(run commandRun) scope.Message {
	sender := run.c.Env.ChatterName()
	touser := sender
	if run.args != "" {
		touser = strings.TrimPrefix(firstArg(run.args), "@")
	}
	return scope.Message{
		User:    strings.TrimPrefix(sender, "@"),
		Sender:  strings.TrimPrefix(sender, "@"),
		Args:    sanitizeVar(run.args),
		Touser:  strings.TrimPrefix(sanitizeVar(touser), "@"),
		Channel: run.c.Env.BroadcasterName(),
	}
}

// logScopeFailure reports a scope whose Plan failed; its tokens then render
// empty (or their fallback) rather than failing the whole reply.
func (p *Pipeline) logScopeFailure(c *module.Context) func(error) {
	return func(err error) {
		p.log.Warn("command scope plan failed", module.BIDField(c.BroadcasterID), zap.Error(err))
	}
}

// counterBumps is the engine half of the counter scope: the grammar (which
// spellings resolve, how a payload folds) lives in scope.Store, and everything
// that needs the run — whose identity rides the bump, the redelivery claim,
// the loyalty store itself — lives here.
//
// The mentioned viewer is resolved lazily and once: a response naming three
// addressed counters looks the mention up in the roster a single time.
type counterBumps struct {
	p   *Pipeline
	run commandRun

	sender   Viewer
	target   Viewer
	resolved bool
}

func newCounterBumps(p *Pipeline, run commandRun) *counterBumps {
	env := run.c.Env
	senderID, _ := strconv.ParseUint(env.ChatterUserID, 10, 64)
	return &counterBumps{
		p: p, run: run,
		sender: Viewer{ID: senderID, Login: env.ChatterUserLogin, Name: env.ChatterUserName},
	}
}

// Bump applies one counter's increment under the run's identity.
func (b *counterBumps) Bump(ctx context.Context, name string, addressed bool) string {
	viewer := b.sender
	if addressed {
		viewer = b.targetViewer()
	}
	return b.p.claimedCounterValue(ctx, b.run.c, name, viewer, b.run.command)
}

// targetViewer is the viewer the command mentions, resolved through the
// roster of chatters this replica has seen speak. A mention nobody has spoken
// where this replica could see falls back to the sender, mirroring how
// {touser} itself defaults to the sender.
func (b *counterBumps) targetViewer() Viewer {
	if b.resolved {
		return b.target
	}
	b.resolved, b.target = true, b.sender
	login := strings.ToLower(strings.TrimPrefix(firstArg(b.run.args), "@"))
	if v, found := b.p.roster.Resolve(b.run.c.BroadcasterID, login); found {
		b.target = v
	}
	return b.target
}

// urlFetches is the engine half of the urlfetch scope: scope.External decides
// which payloads are asked for, this fans them out to gossip.
type urlFetches struct {
	p   *Pipeline
	run commandRun
}

func (f urlFetches) Fetch(ctx context.Context, names []string) map[string]string {
	return f.p.fetchUrlValues(ctx, f.run.c, f.run.command, names)
}

// sanitizeVar neutralizes a user-supplied command variable so it cannot inject
// a leading slash-verb into the expanded response. Control characters (C0 plus
// DEL) are stripped first — an embedded newline would otherwise survive into
// the expansion and emitResponse's per-line split would mint it a fresh line,
// which a leading slash then turns into a remote moderation verb — and
// leading spaces/slashes are trimmed after. The rest is untouched: a URL's
// "http://" keeps its slashes because they are not leading.
func sanitizeVar(s string) string {
	return string(trimLeftSlashSpace(stripControls(rawText(s))))
}

// rawText is text that has been through at most one half of sanitizeVar.
//
// The named type is what stops a caller reaching past sanitizeVar for a
// single half: stripControls alone still lets a leading "/ban" through, and
// trimLeftSlashSpace alone still lets an embedded newline mint the second
// chat line a slash-verb needs, so neither half is safe to hand a template on
// its own. sanitizeVar is the only function here that returns a plain string,
// so a plain string is the only thing that has crossed the whole guard.
type rawText string

// stripControls removes every ASCII control rune before an external value can
// reach a template: an embedded \n or \r would mint extra chat lines through
// emitResponse's per-line split, an ESC poisons terminal/IRC rendering, and a
// NUL truncates downstream writers. Returns s unchanged when it carries none
// (the overwhelmingly common case pays only the scan).
func stripControls(s rawText) rawText {
	i := strings.IndexFunc(string(s), func(r rune) bool { return r < ' ' || r == '\x7f' })
	if i < 0 {
		return s
	}
	out := make([]byte, 0, len(s))
	out = append(out, s[:i]...)
	for _, r := range s[i:] {
		if r >= ' ' && r != '\x7f' {
			out = utf8.AppendRune(out, r)
		}
	}
	return rawText(out)
}

func trimLeftSlashSpace(s rawText) rawText {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '/') {
		i++
	}
	return s[i:]
}
