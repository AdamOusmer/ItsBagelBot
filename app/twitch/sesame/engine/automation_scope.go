// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/pkg/tmpl"
)

const (
	eventTokenPrefix   = "event."
	localTokenName     = "local"
	varTokenName       = "var"
	viewerVarTokenName = "viewervar"
)

type automationScope struct {
	event  map[string]string
	argMap map[string]string
	locals map[string]string
	args   string
	locale string
	points string

	cooldownEndMS  int64
	countdownEndMS int64
}

func (s automationScope) Owns(v scope.Var) bool {
	switch {
	case v.Name == "args":
		return !v.HasPayload
	case v.Name == scope.PointsToken:
		return s.points != "" && !v.HasPayload
	case v.Name == "arg", v.Name == localTokenName:
		return v.HasPayload
	case automation.IsCountdownLeftToken(v):
		return true
	case v.Name == automation.CooldownTokenName:
		return automation.IsCooldownLeftToken(v)
	default:
		return strings.HasPrefix(v.Name, eventTokenPrefix) && !v.HasPayload
	}
}

func (s automationScope) Plan(context.Context, []scope.Var) (scope.Values, error) { return s, nil }

func (s automationScope) Get(v scope.Var) (string, bool) {
	switch v.Name {
	case "args":
		return sanitizeVar(s.args), true
	case scope.PointsToken:
		return s.points, true
	case "arg":
		return sanitizeVar(s.argMap[v.Payload]), true
	case localTokenName:
		return sanitizeVar(s.locals[strings.ToLower(v.Payload)]), true
	case "countdown":
		return countdownDuration(s.locale, s.countdownEndMS), true
	case automation.CooldownTokenName:
		return s.cooldownLeft(), true
	default:
		return sanitizeVar(s.event[strings.TrimPrefix(v.Name, eventTokenPrefix)]), true
	}
}

type variableScope struct {
	store    VariableStore
	channel  VariableOwner
	viewer   VariableOwner
	onViewer bool
}

func (variableScope) Owns(v scope.Var) bool {
	return v.HasPayload && (v.Name == varTokenName || v.Name == viewerVarTokenName)
}

type variableValues map[string]string

func (s variableScope) Plan(ctx context.Context, wants []scope.Var) (scope.Values, error) {
	values := variableValues{}
	if s.store == nil {
		return values, nil
	}
	if err := s.load(ctx, values, variableSource{varTokenName, s.channel}, wants); err != nil {
		return values, err
	}
	if !s.onViewer {
		return values, nil
	}
	return values, s.load(ctx, values, variableSource{viewerVarTokenName, s.viewer}, wants)
}

type variableSource struct {
	token string
	owner VariableOwner
}

func (s variableScope) load(ctx context.Context, into variableValues, source variableSource, wants []scope.Var) error {
	names := wantedVariableNames(source.token, wants)
	if len(names) == 0 {
		return nil
	}
	found, err := s.store.VariablesGet(ctx, source.owner, names)
	for name, value := range found {
		into[source.token+":"+name] = value
	}
	return err
}

func wantedVariableNames(token string, wants []scope.Var) []string {
	var names []string
	for _, want := range wants {
		name, ok := automation.VariableName(want.Payload)
		if ok && want.Name == token {
			names = append(names, name)
		}
	}
	return names
}

// Stored text renders once as a value: it is never lexed, so a saved {arg:1} stays literal.
func (v variableValues) Get(tok scope.Var) (string, bool) {
	return sanitizeVar(v[tok.Name+":"+strings.ToLower(tok.Payload)]), true
}

func (x *actionExec) variableScope() variableScope {
	viewerID := x.run.Viewer.ID
	return variableScope{
		store:    x.p.automations.Variables,
		channel:  VariableOwner{BroadcasterID: x.run.BroadcasterID, Scope: automation.VariableScopeChannel},
		viewer:   VariableOwner{BroadcasterID: x.run.BroadcasterID, ViewerID: viewerID, Scope: automation.VariableScopeViewer},
		onViewer: viewerID != 0,
	}
}

func (x *actionExec) automationScope() automationScope {
	return automationScope{event: x.run.Event, argMap: x.run.ArgMap, locals: x.run.Locals, args: x.run.Args, locale: x.context().Locale, points: x.run.points, cooldownEndMS: x.run.CooldownEndMS, countdownEndMS: x.run.CountdownEndMS}
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
	if (automationScope{points: x.run.points}).Owns(tok) {
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
	x.vars.auto.locals = x.run.Locals
	x.vars.auto.points = x.run.points
	x.vars.auto.cooldownEndMS = x.run.CooldownEndMS
	x.vars.auto.countdownEndMS = x.run.CountdownEndMS
	x.buf = scope.Chain(nil).Render(x.buf[:0], toks, &x.vars)
	return string(x.buf)
}

func (x *actionExec) newRunVars() runVars {
	c := x.context()
	run := commandRun{c: c, command: x.run.Plan.Automation.ID, args: x.run.Args}
	return runVars{
		ready: true,
		auto:  x.automationScope(),
		pure:  scope.Pure{Locale: c.Locale},
		msg:   messageVars(run),
	}
}

func (x *actionExec) expand(text string) string {
	if strings.IndexByte(text, '{') < 0 {
		return text
	}
	run := commandRun{c: x.context(), command: x.run.Plan.Automation.ID, args: x.run.Args}
	r := textRender{
		lex: x.lex,
		chainOf: func(toks []tmpl.Token) scope.Chain {
			return append(scope.Chain{x.automationScope(), x.variableScope()}, x.p.commandChain(x.ctx, run, toks)...)
		},
		onErr: x.p.logScopeFailure(run.c),
	}
	flat, toks := r.tokens(x.ctx, text)
	switch {
	case !hasVarToken(toks):
		return flat
	case x.resident(toks):
		return x.renderResident(toks)
	}
	return r.render(x.ctx, toks)
}
