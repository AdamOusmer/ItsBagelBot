// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/pkg/tmpl"
)

const eventTokenPrefix = "event."

type automationScope struct {
	event  map[string]string
	argMap map[string]string
	args   string
}

func (automationScope) Owns(v scope.Var) bool {
	switch {
	case v.Name == "args":
		return !v.HasPayload
	case v.Name == "arg":
		return v.HasPayload
	default:
		return strings.HasPrefix(v.Name, eventTokenPrefix) && !v.HasPayload
	}
}

func (s automationScope) Plan(context.Context, []scope.Var) (scope.Values, error) { return s, nil }

func (s automationScope) Get(v scope.Var) (string, bool) {
	switch v.Name {
	case "args":
		return sanitizeVar(s.args), true
	case "arg":
		return sanitizeVar(s.argMap[v.Payload]), true
	default:
		return sanitizeVar(s.event[strings.TrimPrefix(v.Name, eventTokenPrefix)]), true
	}
}

func (p *Pipeline) automationContext(ctx context.Context, run *AutomationRun) *module.Context {
	c := GetContext()
	c.Regress = run.Regress
	c.BroadcasterID = run.BroadcasterID
	c.Log = p.log
	c.Locale = run.Locale
	c.LocaleLookup = p.localeLookup
	if run.Env != nil {
		c.Env = *run.Env
	}
	if c.Env.BroadcasterUserID == "" {
		c.Env.BroadcasterUserID = run.Channel
	}
	applyViewer(&c.Env, run.Viewer)
	c.EnsureLocale(ctx)
	return c
}

func applyViewer(env *lane.Envelope, v Viewer) {
	if v.ID != 0 {
		env.ChatterUserID = strconv.FormatUint(v.ID, 10)
	}
	if v.Login != "" {
		env.ChatterUserLogin = v.Login
	}
	if v.Name != "" {
		env.ChatterUserName = v.Name
	}
}

func (x *actionExec) context() *module.Context {
	if x.tctx == nil {
		x.tctx = x.p.automationContext(x.ctx, x.run)
	}
	return x.tctx
}

type runVars struct {
	ready bool
	auto  automationScope
	pure  scope.Pure
	msg   scope.Message
}

func (v *runVars) Get(tok scope.Var) (string, bool) {
	switch {
	case v.auto.Owns(tok):
		return v.auto.Get(tok)
	case v.pure.Owns(tok):
		return v.pure.Get(tok)
	case v.msg.Owns(tok):
		return v.msg.Get(tok)
	}
	return "", false
}

func (x *actionExec) lex(text string) []tmpl.Token {
	if toks, ok := x.run.Plan.Templates[text]; ok {
		return toks
	}
	return tmpl.Lex(text)
}

func (x *actionExec) resident(toks []tmpl.Token) bool {
	for _, tok := range toks {
		if tok.Kind == tmpl.KindVar && !x.residentVar(tok) {
			return false
		}
	}
	return true
}

func (x *actionExec) residentVar(tok tmpl.Token) bool {
	if _, conditional := tok.Cond(); conditional {
		return false
	}
	if (automationScope{}).Owns(tok) {
		return true
	}
	if _, namespaced := x.p.namespaceModule(tok); namespaced {
		return false
	}
	return (scope.Pure{}).Owns(tok) || (scope.Message{}).Owns(tok)
}

func (x *actionExec) renderResident(toks []tmpl.Token) string {
	if !x.vars.ready {
		x.vars = x.newRunVars()
	}
	x.buf = scope.Chain(nil).Render(x.buf[:0], toks, &x.vars)
	return string(x.buf)
}

func (x *actionExec) newRunVars() runVars {
	c := x.context()
	run := commandRun{c: c, command: x.run.Plan.Automation.ID, args: x.run.Args}
	return runVars{
		ready: true,
		auto:  automationScope{event: x.run.Event, argMap: x.run.ArgMap, args: x.run.Args},
		pure:  scope.Pure{Locale: c.Locale},
		msg:   messageVars(run),
	}
}

func (x *actionExec) expand(text string) string {
	if strings.IndexByte(text, '{') < 0 {
		return text
	}
	toks := x.lex(text)
	if !hasVarToken(toks) {
		return text
	}
	if x.resident(toks) {
		return x.renderResident(toks)
	}
	run := commandRun{c: x.context(), command: x.run.Plan.Automation.ID, args: x.run.Args}
	chain := append(scope.Chain{automationScope{event: x.run.Event, argMap: x.run.ArgMap, args: x.run.Args}},
		x.p.commandChain(x.ctx, run, tmpl.WithCondRefs(toks))...)
	values := chain.Plan(x.ctx, toks, x.p.logScopeFailure(run.c))
	buf := GetBuf()
	buf = chain.Render(buf, toks, values)
	out := string(buf)
	PutBuf(buf)
	return out
}
