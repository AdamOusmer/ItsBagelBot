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
	"ItsBagelBot/pkg/tmpl"

	"go.uber.org/zap"
)

func (p *Pipeline) commandChain(ctx context.Context, run commandRun, toks []tmpl.Token) scope.Chain {
	chain := scope.Chain{
		scope.Pure{Locale: run.c.Locale},
		messageVars(run),
		scope.Uses{Count: run.uses},
		p.chattersScope(run.c, toks),
	}
	if emotes, mounted := p.emotesScope(toks); mounted {
		chain = append(chain, emotes)
	}
	if channel, mounted := p.channelScope(ctx, run.c, toks); mounted {
		chain = append(chain, channel)
	}
	if viewer, mounted := p.viewerScope(ctx, run.c, toks); mounted {
		chain = append(chain, viewer)
	}
	if mods, mounted := p.moduleScope(ctx, run.c, toks); mounted {
		chain = append(chain, mods)
	}
	if p.loyalty != nil {
		chain = append(chain, scope.Store{Peeks: newCounterPeeks(p, run)})
	}
	if p.customFetch != nil {
		chain = append(chain, scope.External{
			Fetcher: urlFetches{p: p, run: run},
			Max:     maxUrlFetchTokens,
		})
	}
	return chain
}

type commandRun struct {
	c       *module.Context
	command string
	args    string

	uses uint64
}

func messageVars(run commandRun) scope.Message {
	sender := run.c.Env.ChatterName()
	touser := sender
	if run.args != "" {
		touser = strings.TrimPrefix(firstArg(run.args), "@")
	}
	return scope.Message{
		User:    strings.TrimPrefix(sender, "@"),
		Sender:  strings.TrimPrefix(sender, "@"),
		Words:   sanitizeWords(run.args),
		Touser:  strings.TrimPrefix(sanitizeVar(touser), "@"),
		Channel: run.c.Env.BroadcasterName(),
		UserID:  run.c.Env.ChatterUserID,
		Login:   run.c.Env.ChatterUserLogin,
		Command: run.command,
	}
}

func sanitizeWords(args string) []string {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return nil
	}
	words := make([]string, len(fields))
	for i, f := range fields {
		words[i] = sanitizeVar(f)
	}
	return words
}

func (p *Pipeline) logScopeFailure(c *module.Context) func(error) {
	return func(err error) {
		p.log.Warn("command scope plan failed", module.BIDField(c.BroadcasterID), zap.Error(err))
	}
}

type counterPeeks struct {
	p   *Pipeline
	run commandRun

	sender   Viewer
	target   Viewer
	resolved bool
}

func newCounterPeeks(p *Pipeline, run commandRun) *counterPeeks {
	env := run.c.Env
	senderID, _ := strconv.ParseUint(env.ChatterUserID, 10, 64)
	return &counterPeeks{
		p: p, run: run,
		sender: Viewer{ID: senderID, Login: env.ChatterUserLogin, Name: env.ChatterUserName},
	}
}

func (b *counterPeeks) Peek(ctx context.Context, name string, addressed bool) string {
	return CounterPeekValue(ctx, b.p.loyalty, CounterTarget{
		BroadcasterID: b.run.c.BroadcasterID,
		Name:          name,
		ViewerID:      b.viewerFor(addressed).ID,
		Command:       b.run.command,
	})
}

func (b *counterPeeks) viewerFor(addressed bool) Viewer {
	if addressed {
		return b.targetViewer()
	}
	return b.sender
}

func (b *counterPeeks) targetViewer() Viewer {
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

type urlFetches struct {
	p   *Pipeline
	run commandRun
}

func (f urlFetches) Fetch(ctx context.Context, names []string) map[string]string {
	return f.p.fetchUrlValues(ctx, f.run.c, f.run.command, names)
}

func sanitizeVar(s string) string {
	return string(trimLeftSlashSpace(stripControls(rawText(s))))
}

type rawText string

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
