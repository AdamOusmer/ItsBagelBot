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

type timerRun struct {
	ref     timerRef
	locale  string
	firedAt time.Time
}

func timerRunIdentity(ref timerRef, firedAt time.Time) string {
	return "timer:" + strconv.FormatUint(ref.broadcasterID, 10) + ":" + ref.id + ":" + strconv.FormatInt(firedAt.UnixNano(), 10)
}

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
	if p.customFetch != nil {
		chain = append(chain, scope.External{
			Fetcher: urlFetches{p: p, run: commandRun{c: c, command: "timer:" + run.ref.id}},
			Max:     maxUrlFetchTokens,
		})
	}
	return chain
}

func (p *Pipeline) logTimerScopeFailure(ref timerRef) func(error) {
	return func(err error) {
		p.log.Warn("timer scope plan failed", module.BIDField(ref.broadcasterID), zap.String("timer_id", ref.id), zap.Error(err))
	}
}

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

func defangTimerSlashLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = defangLeadingSlash(line)
	}
	return strings.Join(lines, "\n")
}

func defangLeadingSlash(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '/') {
		i++
	}
	return s[i:]
}
