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
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/pkg/idempotency"
	"ItsBagelBot/pkg/tmpl"

	"go.uber.org/zap"
)

func (p *Pipeline) dispatchCommand(ctx context.Context, c *module.Context, views map[string]projection.ModuleView, emit module.Emit) error {
	before := c.Emitted
	defer func() { c.CommandAnswered = c.Emitted > before }()
	input, ok := parseCommand(c.Env.Text)
	if !ok {
		return nil
	}
	name := strings.ToLower(input.name)
	owners, num := p.registry.resolveOwners(name)
	if c.Env.Origin == "trial" && !trialCommand(owners, name) {
		return nil
	}
	p.bindCommand(c, views, name)
	if bc, ok := p.claimOwner(ctx, owners, views, c); ok {
		emittedBefore := c.Emitted
		err := p.runBaked(ctx, c, bakedCommand{cmd: bc.Cmd, num: num, args: input.args}, emit)
		return p.finishBaked(ctx, c, bakedRun{bc: bc, emittedBefore: emittedBefore, err: err})
	}
	if c.Env.Origin == "trial" {
		return nil
	}
	claimed, err := p.runCustom(ctx, c, input.args, emit)
	if claimed || err != nil {
		return err
	}
	p.runAutomationCommand(ctx, c, recipeCommand{input: input, views: views}, emit)
	return nil
}

func (p *Pipeline) bindCommand(c *module.Context, views map[string]projection.ModuleView, name string) {
	c.Command = name
	c.SpeechPolicy = automodConfigFrom(views, false).SpeechPolicy
	if name == voiceplan.ChatCommand {
		c.BlocksText = automodConfigFrom(views, false).BlocksText
	}
}

type bakedRun struct {
	bc            BoundCommand
	emittedBefore int
	err           error
}

func (p *Pipeline) finishBaked(ctx context.Context, c *module.Context, r bakedRun) error {
	if r.err == nil {
		p.recordBaked(ctx, c, r.bc, r.emittedBefore)
	}
	return r.err
}

func trialCommand(owners []BoundCommand, name string) bool {
	return (len(owners) > 0 && owners[0].Owner.Trial) || trialReadOnlyCommand(name)
}

func (p *Pipeline) claimOwner(ctx context.Context, owners []BoundCommand, views map[string]projection.ModuleView, c *module.Context) (BoundCommand, bool) {
	if len(owners) == 0 {
		return BoundCommand{}, false
	}
	c.ModuleLookup = p.moduleLookup(views, c)
	c.TriggerDisabledLookup = p.triggerDisabledLookup(views)
	declined := -1
	for i, bc := range owners {
		if !p.ownerActive(bc.Owner, views, c) {
			continue
		}
		if bc.Cmd.When == nil || bc.Cmd.When(ctx, c) {
			return bc, true
		}
		if declined < 0 && !bc.Cmd.Yield {
			declined = i
		}
	}
	return p.fallbackOwner(owners, views, c, declined)
}

func (p *Pipeline) ownerActive(owner module.Module, views map[string]projection.ModuleView, c *module.Context) bool {
	if !p.enabled(owner, views, c) {
		return false
	}
	return owner.Kind == module.KindCore || !triggerDisabled(c.Config, module.TriggerName(c.Command))
}

func (p *Pipeline) fallbackOwner(owners []BoundCommand, views map[string]projection.ModuleView, c *module.Context, declined int) (BoundCommand, bool) {
	if declined < 0 {
		return BoundCommand{}, false
	}
	p.enabled(owners[declined].Owner, views, c)
	return owners[declined], true
}

func (p *Pipeline) moduleLookup(views map[string]projection.ModuleView, c *module.Context) func(string) bool {
	return func(name string) bool {
		m, ok := p.registry.byName[name]
		if !ok {
			return false
		}
		config := c.Config
		on := p.enabled(m, views, c)
		c.Config = config
		return on
	}
}

func (p *Pipeline) triggerDisabledLookup(views map[string]projection.ModuleView) func(module.ModuleName, module.TriggerName) bool {
	return func(name module.ModuleName, trigger module.TriggerName) bool {
		mv, ok := views[string(name)]
		return ok && triggerDisabled(mv.Configs, trigger)
	}
}

func trialReadOnlyCommand(name string) bool {
	// Followage and uptime require registered-channel admission in Outgress.
	// Trial chat must stop here instead of issuing lookups that cannot succeed.
	switch name {
	case "ping", "itsbagelbot", "clip", "accountage",
		"bagels", "fed", "bagelcount", "bagelboard", "feedboard", "bagellb",
		"title", "settitle", "game", "setgame", "tags", "settags", "commercial", "ad", "marker":
		return true
	default:
		return false
	}
}

type bakedCommand struct {
	cmd       module.Command
	num, args string
}

func (p *Pipeline) runBaked(ctx context.Context, c *module.Context, command bakedCommand, emit module.Emit) error {
	cmd := command.cmd
	rule, err := p.builtinGate(ctx, c, cmd)
	if err != nil {
		return err
	}
	if pass, err := p.gate(ctx, c, rule); err != nil || !pass {
		return err
	}
	c.Num = command.num
	p.ensureLocale(ctx, c)
	run := commandRun{c: c, command: cmd.Name, args: command.args}
	var emitErr error
	err = cmd.Run(ctx, c, command.args, func(o *module.Output) {
		if emitErr != nil {
			return
		}
		_, emitErr = p.emitCommand(ctx, run, o, emit)
	})
	if err != nil {
		return err
	}
	if emitErr == nil {
		c.Engaged = true
	}
	return emitErr
}

func (p *Pipeline) activeCustom(ctx context.Context, c *module.Context, name string) (projection.Command, bool, error) {
	cc, found, err := p.proj.Command(ctx, c.BroadcasterID, name)
	if err != nil || !found {
		return cc, false, err
	}
	return cc, cc.IsActive && cc.Response != "", nil
}

func (p *Pipeline) runCustom(ctx context.Context, c *module.Context, args string, emit module.Emit) (bool, error) {
	cc, ok, err := p.activeCustom(ctx, c, c.Command)
	if err != nil || !ok {
		return false, err
	}

	pass, err := p.gate(ctx, c, gateRule{
		name:          strings.ToLower(cc.Name),
		allowedUserID: cc.AllowedUserID,
		perm:          module.ParsePerm(cc.Perm),
		liveOnly:      cc.StreamOnlineOnly,
		cooldown:      time.Duration(cc.Cooldown) * time.Second,
		userCooldown:  time.Duration(cc.UserCooldown) * time.Second,
	})
	if err != nil || !pass {
		return true, err
	}

	p.log.Debug("command matched",
		zap.String("command", c.Command),
		zap.String("regress", c.Regress.String()),
		module.BIDField(c.BroadcasterID),
	)

	run := commandRun{c: c, command: cc.Name, args: args, uses: cc.Uses}
	before := c.Emitted
	_, err = p.emitCommand(ctx, run, &module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: c.Env.BroadcasterUserID,
		Text:          cc.Response,
	}, emit)
	if err != nil || c.Emitted == before {
		return true, err
	}

	p.recordUse(ctx, c, cc.Name)
	id := c.BroadcasterID
	p.recordCommandAnalytics(ctx, c, func(ctx context.Context) {
		p.analytics.AddContext(ctx, catalog.CommandUses, 1, catalog.CustomCommand)
		p.analytics.DistinctContext(ctx, catalog.CommandChannels, id, catalog.CustomCommand)
	})
	p.bumpCommandCounter(ctx, c, cc, args)
	c.Engaged = true
	return true, nil
}

func (p *Pipeline) bumpCommandCounter(ctx context.Context, c *module.Context, cc projection.Command, args string) {
	if cc.BumpCounter == "" || p.loyalty == nil {
		return
	}
	senderID, _ := strconv.ParseUint(c.Env.ChatterUserID, 10, 64)
	sender := Viewer{ID: senderID, Login: c.Env.ChatterUserLogin, Name: c.Env.ChatterUserName}
	p.claimedCounterValue(ctx, c, CounterBump{
		BroadcasterID: c.BroadcasterID,
		Name:          cc.BumpCounter,
		Viewer:        sender,
		Command:       cc.Name,
		Delta:         counterStep(c, args),
	})
}

const counterStepMax = 1_000_000

func counterStep(c *module.Context, args string) int64 {
	if !c.Chatter().Allows(module.RoleModerator) {
		return 1
	}
	n, err := strconv.ParseInt(firstArg(args), 10, 64)
	if err != nil || !validCounterStep(n) {
		return 1
	}
	return n
}

func validCounterStep(n int64) bool {
	return n != 0 && n <= counterStepMax && n >= -counterStepMax
}

func (p *Pipeline) recordUse(ctx context.Context, c *module.Context, name string) {
	if p.uses == nil {
		return
	}
	err := p.dedup.Effect(ctx, EffectRef{Identity: EventIdentity(&c.Env), Effect: effectUse}, func(ctx context.Context) error {
		p.uses.RecordContext(ctx, useKey{userID: c.BroadcasterID, name: name})
		return nil
	})
	idempotency.Fail(ctx, err)
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
	r := textRender{
		lex:     tmpl.Lex,
		chainOf: func(toks []tmpl.Token) scope.Chain { return p.commandChain(ctx, run, toks) },
		onErr:   p.logScopeFailure(run.c),
	}
	o.Text = r.expand(ctx, o.Text)
}

func (p *Pipeline) chatLines(o *module.Output) []module.Output {
	return p.appendChatLines(make([]module.Output, 0, validate.MaxResponseLines), o)
}

func (p *Pipeline) appendChatLines(dst []module.Output, o *module.Output) []module.Output {
	parentID := o.MsgID
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
		out.MsgID = parentID
		if p.prepareCommand(out) {
			dst = append(dst, *out)
			parentID = ""
		}
		PutOutput(out)
	}
	return dst
}

func (p *Pipeline) claimedCounterValue(ctx context.Context, c *module.Context, b CounterBump) string {
	bump := func() (int64, error) {
		return p.loyalty.CounterBump(ctx, b)
	}
	fail := func(err error) string {
		idempotency.Fail(ctx, err)
		p.log.Warn("counter token bump failed",
			module.BIDField(c.BroadcasterID),
			zap.String("counter", b.Name),
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
	var value int64
	applied := false
	err := p.dedup.Effect(ctx, EffectRef{Identity: EventIdentity(&c.Env), Effect: CounterEffect(b.Name)}, func(effectCtx context.Context) error {
		var err error
		value, err = p.loyalty.CounterBump(effectCtx, b)
		applied = err == nil
		return err
	})
	if err != nil {
		idempotency.Fail(ctx, err)
		return fail(err)
	}
	if !applied {
		return CounterPeekValue(ctx, p.loyalty, CounterTarget{
			BroadcasterID: b.BroadcasterID,
			Name:          b.Name,
			ViewerID:      b.Viewer.ID,
			Command:       b.Command,
		})
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
	userCooldown  time.Duration
}

func (p *Pipeline) gate(ctx context.Context, c *module.Context, r gateRule) (bool, error) {
	if !permits(c, r.allowedUserID, r.perm) {
		p.recordBlocked(ctx, c, "perm", nil)
		return false, nil
	}
	if ok, err := p.liveOK(ctx, c, r.liveOnly); !ok {
		p.recordBlocked(ctx, c, "offline", err)
		return false, err
	}
	if c.Env.Origin == "trial" {
		return true, nil
	}
	ok, err := p.cooldownOK(ctx, c, r)
	if !ok {
		p.recordBlocked(ctx, c, "cooldown", err)
	}
	return ok, err
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

// Both windows are claimed together or not at all, so a viewer still inside
// their own window never starts the channel window, and the reverse.
func (p *Pipeline) cooldownOK(ctx context.Context, c *module.Context, r gateRule) (bool, error) {
	switch {
	case r.cooldown <= 0 && r.userCooldown <= 0:
		return true, nil
	case r.userCooldown <= 0:
		return p.cooldown.Allow(ctx, cooldownKey(c.BroadcasterID, r.name), r.cooldown)
	case r.cooldown <= 0:
		return p.cooldown.Allow(ctx, r.viewerCooldownKey(c), r.userCooldown)
	default:
		return p.cooldown.AllowAll(ctx, []CooldownWindow{
			{Key: cooldownKey(c.BroadcasterID, r.name), TTL: r.cooldown},
			{Key: r.viewerCooldownKey(c), TTL: r.userCooldown},
		})
	}
}

func (r gateRule) viewerCooldownKey(c *module.Context) string {
	buf := GetBuf()
	buf = append(buf, "cooldown:cmduser:"...)
	buf = strconv.AppendUint(buf, c.BroadcasterID, 10)
	buf = append(buf, ':')
	buf = append(buf, c.Env.ChatterUserID...)
	buf = append(buf, ':')
	buf = append(buf, r.name...)
	key := string(buf)
	PutBuf(buf)
	return key
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
