// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/pkg/tmpl"

	"go.uber.org/zap"
)

func (p *Pipeline) dispatchCommand(ctx context.Context, c *module.Context, views map[string]projection.ModuleView, emit module.Emit) error {
	name, args, ok := parseCommand(c.Env.Text)
	if !ok {
		return nil
	}
	bc, num, isBaked := p.registry.ResolveCommand(name)
	if c.Env.Origin == "trial" && !trialCommand(bc, isBaked, name) {
		return nil
	}
	c.Command = name
	if isBaked && p.enabled(bc.Owner, views, c) {
		return p.runBaked(ctx, c, bc.Cmd, num, args, emit)
	}
	if c.Env.Origin == "trial" {
		return nil
	}
	return p.runCustom(ctx, c, name, args, emit)
}

func trialCommand(bc BoundCommand, isBaked bool, name string) bool {
	return (isBaked && bc.Owner.Trial) || trialReadOnlyCommand(name)
}

func trialReadOnlyCommand(name string) bool {
	switch name {
	case "ping", "source", "itsbagelbot", "clip", "followage", "accountage", "uptime",
		"bagels", "fed", "bagelcount", "bagelboard", "feedboard", "bagellb",
		"title", "settitle", "game", "setgame", "tags", "settags", "commercial", "ad", "marker":
		return true
	default:
		return false
	}
}

func (p *Pipeline) runBaked(ctx context.Context, c *module.Context, cmd module.Command, num, args string, emit module.Emit) error {
	pass, err := p.gate(ctx, c, gateRule{cmd.Name, cmd.AllowedUserID, cmd.Perm, cmd.LiveOnly, cmd.Cooldown})
	if err != nil || !pass {
		return err
	}
	c.Num = num
	if u, uerr := p.proj.User(ctx, c.BroadcasterID); uerr == nil {
		c.Locale = u.Locale
	}
	run := commandRun{c: c, command: cmd.Name, args: args}
	var emitErr error
	err = cmd.Run(ctx, c, args, func(o *module.Output) {
		if emitErr != nil {
			return
		}
		_, emitErr = p.emitCommand(ctx, run, o, emit)
	})
	if err != nil {
		return err
	}
	return emitErr
}

func (p *Pipeline) runCustom(ctx context.Context, c *module.Context, name, args string, emit module.Emit) error {
	cc, found, err := p.proj.Command(ctx, c.BroadcasterID, name)
	if err != nil || !found || !cc.IsActive {
		return err
	}
	if cc.Response == "" {
		return nil
	}

	rule := gateRule{name, cc.AllowedUserID, module.ParsePerm(cc.Perm), cc.StreamOnlineOnly, time.Duration(cc.Cooldown) * time.Second}
	pass, err := p.gate(ctx, c, rule)
	if err != nil || !pass {
		return err
	}

	p.log.Debug("command matched",
		zap.String("command", name),
		zap.String("regress", c.Regress.String()),
		module.BIDField(c.BroadcasterID),
	)

	run := commandRun{c: c, command: cc.Name, args: args, uses: cc.Uses}
	emitted, err := p.emitCommand(ctx, run, &module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: c.Env.BroadcasterUserID,
		Text:          cc.Response,
	}, emit)
	if err != nil || !emitted {
		return err
	}

	p.recordUse(ctx, c, cc.Name)
	p.bumpCommandCounter(ctx, c, cc)
	return nil
}

func (p *Pipeline) bumpCommandCounter(ctx context.Context, c *module.Context, cc projection.Command) {
	if cc.BumpCounter == "" || p.loyalty == nil {
		return
	}
	senderID, _ := strconv.ParseUint(c.Env.ChatterUserID, 10, 64)
	sender := Viewer{ID: senderID, Login: c.Env.ChatterUserLogin, Name: c.Env.ChatterUserName}
	p.claimedCounterValue(ctx, c, cc.BumpCounter, sender, cc.Name)
}

func (p *Pipeline) recordUse(ctx context.Context, c *module.Context, name string) {
	if p.uses == nil || p.dedup.Duplicate(ctx, EffectRef{Identity: EventIdentity(&c.Env), Effect: effectUse}) {
		return
	}
	p.uses.Record(c.BroadcasterID, name)
}

func blankLine(line string) bool {
	return strings.TrimSpace(line) == ""
}

func (p *Pipeline) emitPreparedResponse(c *module.Context, outputs []module.Output, emit module.Emit) (bool, error) {
	switch len(outputs) {
	case 0:
		return false, nil
	case 1:
		emit(&outputs[0])
		return true, nil
	}

	batchID := c.Env.MsgID
	if batchID == "" {
		id, err := utils.NewID()
		if err != nil {
			return false, err
		}
		batchID = id.String()
	}
	batch := GetOutput()
	batch.Type = outgress.TypeBatch
	batch.BroadcasterID = c.Env.BroadcasterUserID
	batch.BatchID = batchID
	batch.Items = outputs
	emit(batch)
	PutOutput(batch)
	return true, nil
}

func (p *Pipeline) prepareCommand(o *module.Output) bool {
	Translate(o)
	return !isEmptyAction(o) && !p.floorSuppressed(o)
}

// Expand only here: newEmit would re-lex {args} and turn a viewer's typed "{user}" into a token.
func (p *Pipeline) emitCommand(ctx context.Context, run commandRun, o *module.Output, emit module.Emit) (bool, error) {
	p.expandCommandText(ctx, run, o)
	if o.Type == outgress.TypeChat {
		return p.emitPreparedResponse(run.c, p.chatLines(o), emit)
	}
	if !p.prepareCommand(o) {
		return false, nil
	}
	emit(o)
	return true, nil
}

func (p *Pipeline) expandCommandText(ctx context.Context, run commandRun, o *module.Output) {
	switch o.Type {
	case outgress.TypeChat, outgress.TypeAnnounce, outgress.TypePin:
	default:
		return
	}
	if o.Text == "" {
		return
	}
	toks := tmpl.Lex(o.Text)
	if !hasVarToken(toks) {
		return
	}
	chain := p.commandChain(ctx, run, tmpl.WithCondRefs(toks))
	values := chain.Plan(ctx, toks, p.logScopeFailure(run.c))
	buf := GetBuf()
	buf = chain.Render(buf, toks, values)
	o.Text = string(buf)
	PutBuf(buf)
}

func hasVarToken(toks []tmpl.Token) bool {
	for _, tok := range toks {
		if tok.Kind == tmpl.KindVar {
			return true
		}
	}
	return false
}

func (p *Pipeline) chatLines(o *module.Output) []module.Output {
	outputs := make([]module.Output, 0, validate.MaxResponseLines)
	lines := 0
	for line := range strings.SplitSeq(o.Text, "\n") {
		if blankLine(line) {
			continue
		}
		lines++
		if lines > validate.MaxResponseLines {
			break
		}
		out := GetOutput()
		out.Type = outgress.TypeChat
		out.BroadcasterID = o.BroadcasterID
		out.Text = line
		if p.prepareCommand(out) {
			outputs = append(outputs, *out)
		}
		PutOutput(out)
	}
	return outputs
}

func (p *Pipeline) claimedCounterValue(ctx context.Context, c *module.Context, name string, viewer Viewer, command string) string {
	bump := func() (int64, error) {
		return p.loyalty.CounterBump(ctx, CounterBump{
			BroadcasterID: c.BroadcasterID,
			Name:          name,
			Viewer:        viewer,
			Command:       command,
			Delta:         1,
		})
	}
	fail := func(err error) string {
		p.log.Warn("counter token bump failed",
			module.BIDField(c.BroadcasterID),
			zap.String("counter", name),
			zap.Error(err),
		)
		return ""
	}
	if p.dedup == nil {
		value, err := bump()
		if err != nil {
			return fail(err)
		}
		return strconv.FormatInt(value, 10)
	}
	dup, release := p.dedup.Claim(ctx, EffectRef{Identity: EventIdentity(&c.Env), Effect: CounterEffect(name)})
	if dup {
		return CounterPeekValue(ctx, p.loyalty, CounterTarget{
			BroadcasterID: c.BroadcasterID,
			Name:          name,
			ViewerID:      viewer.ID,
			Command:       command,
		})
	}
	value, err := bump()
	if err != nil {
		release()
		return fail(err)
	}
	return strconv.FormatInt(value, 10)
}

func firstArg(args string) string {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

type gateRule struct {
	name          string
	allowedUserID string
	perm          module.Role
	liveOnly      bool
	cooldown      time.Duration
}

func (p *Pipeline) gate(ctx context.Context, c *module.Context, r gateRule) (bool, error) {
	if !permits(c, r.allowedUserID, r.perm) {
		return false, nil
	}
	if ok, err := p.liveOK(ctx, c, r.liveOnly); !ok {
		return false, err
	}
	if c.Env.Origin == "trial" {
		return true, nil
	}
	return p.cooldownOK(ctx, c.BroadcasterID, r.name, r.cooldown)
}

func permits(c *module.Context, allowedUserID string, perm module.Role) bool {
	if allowedUserID != "" {
		return c.Env.ChatterUserID == allowedUserID
	}
	return c.Chatter().Allows(perm)
}

func (p *Pipeline) liveOK(ctx context.Context, c *module.Context, liveOnly bool) (bool, error) {
	if !liveOnly {
		return true, nil
	}
	return p.live.IsLive(ctx, c.BroadcasterID)
}

func (p *Pipeline) cooldownOK(ctx context.Context, broadcasterID uint64, name string, cooldown time.Duration) (bool, error) {
	if cooldown <= 0 {
		return true, nil
	}
	return p.cooldown.Allow(ctx, cooldownKey(broadcasterID, name), cooldown)
}

func CommandCooldownKey(broadcasterID uint64, name string) string {
	return cooldownKey(broadcasterID, name)
}

func cooldownKey(broadcasterID uint64, name string) string {
	buf := GetBuf()
	buf = append(buf, "cooldown:cmd:"...)
	buf = strconv.AppendUint(buf, broadcasterID, 10)
	buf = append(buf, ':')
	buf = append(buf, name...)
	key := string(buf)
	PutBuf(buf)
	return key
}
