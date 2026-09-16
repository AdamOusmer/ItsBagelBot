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

// dispatchCommand is the command stage the pipeline runs for every chat line. It
// parses the "!command", looks it up first in the registry's bound command index
// and then in the broadcaster's custom commands, applies the one shared gate
// (permission, live-only, cooldown), and runs the winner. It is the folded-in
// command router: unlike the worker it is not a module, so it reads the registry
// directly and needs no Bind. A non-command line returns nil with no work.
//
// A baked command is first gated by its owning module's enable state (the same
// enabled() check event handlers pass), so a command on a disabled module never
// runs — the trigger instead falls through to the broadcaster's custom
// commands, so an opt-in module can ship friendly triggers without reserving
// them fleet-wide. The gate also wires the module's config into the Context
// before the command runs. views is the broadcaster's ModuleView set (nil when
// the chat path needs none, i.e. only core command owners).
func (p *Pipeline) dispatchCommand(ctx context.Context, c *module.Context, views map[string]projection.ModuleView, emit module.Emit) error {
	name, args, ok := parseCommand(c.Env.Text)
	if !ok {
		return nil
	}
	// Recorded for the post-stage observer hook (see engine/observe.go). The
	// name is a view into the pooled payload; the hook clones before it hands
	// the event to anything that outlives Process.
	c.Command = name
	if bc, num, isBaked := p.registry.ResolveCommand(name); isBaked {
		if p.enabled(bc.Owner, views, c) {
			return p.runBaked(ctx, c, bc.Cmd, num, args, emit)
		}
		// The owner module is off: fall through to the broadcaster's custom
		// commands so an opt-in module's trigger (e.g. !daily) never reserves the
		// name on channels that did not enable it.
	}
	return p.runCustom(ctx, c, name, args, emit)
}

// runBaked gates and runs a command a module owns. Every output the command
// emits is routed through emitCommand, which always lexes the reply and then
// applies the shared slash-verb middleware, so a baked command can write
// "/announce ..." the same way a custom one does. num is the inline numeric
// suffix the trigger absorbed ("" when none / not a NumericSuffix command); it
// is exposed on the Context for the command to read.
func (p *Pipeline) runBaked(ctx context.Context, c *module.Context, cmd module.Command, num, args string, emit module.Emit) error {
	pass, err := p.gate(ctx, c, gateRule{cmd.Name, cmd.AllowedUserID, cmd.Perm, cmd.LiveOnly, cmd.Cooldown})
	if err != nil || !pass {
		return err
	}
	c.Num = num
	// Resolve the broadcaster's UI locale so baked commands can localize replies.
	// Only for commands that actually run (past the gate); the read is cache
	// fronted, and any miss leaves Locale empty (default language).
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

// runCustom resolves a broadcaster's custom command, gates it with the same rule
// as a baked command, then hands the stored template to emitCommand — the same
// lexer, line split, and slash-verb path baked replies use.
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

	// Count the successful run. cc.Name is the canonical key (an alias lookup
	// resolves to it), so alias invocations all count against the one command.
	p.recordUse(ctx, c, cc.Name)
	return nil
}

// recordUse counts one successful command run. The reporter sums ticks locally
// and publishes one event per command per flush window, so chat spam never
// floods NATS. It is deduped so a redelivered command line does not inflate the
// summed count: the use counter is one of the effects that is not naturally
// idempotent, and a quorum loss redelivers whatever was in flight.
func (p *Pipeline) recordUse(ctx context.Context, c *module.Context, name string) {
	if p.uses == nil || p.dedup.Duplicate(ctx, EffectRef{Identity: EventIdentity(&c.Env), Effect: effectUse}) {
		return
	}
	p.uses.Record(c.BroadcasterID, name)
}

// blankLine reports whether an expanded line has nothing left to say.
//
// Whitespace-only counts as blank, not just empty, and that is what a
// conditional needs: "{if:2:and {2}}" on a one-word invocation renders a line
// holding one space, and a space is not a chat message. The drop happens
// BEFORE the line is counted against validate.MaxResponseLines, so a response
// whose middle line vanishes still sends every line the broadcaster wrote —
// the alternative (count then drop) would silently eat the fifth line of a
// five-line reply the moment one of the first four went quiet.
//
// It is the same reason the blank-line skip existed before conditionals: a
// stored response with a double newline in it must not publish an empty chat
// message. This only widens "empty" to "nothing visible".
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

// emitCommand is the one command emit path. Baked modules and custom-command
// templates both hand it a chat body; it lexes, splits lines, then translates
// each line's slash-verb. Two expanders is how songqueue posted a literal
// "@{user}" — the baked path skipped the lexer the custom path already had.
//
// Expansion lives HERE, not in newEmit. newEmit also publishes event-handler
// lines and would re-lex a value already substituted into {args}, turning a
// viewer's "{user}" into a token, which is the injection sanitizeVar exists
// to keep as literal text.
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

// expandCommandText runs one command body through the command lexer. A body
// with no '{' is left alone (ping-style copy, provider errors). Plan happens
// once, before chatLines walks the result, so no lookup can hide inside the
// loop that writes a chat line.
func (p *Pipeline) expandCommandText(ctx context.Context, run commandRun, o *module.Output) {
	switch o.Type {
	case outgress.TypeChat, outgress.TypeAnnounce, outgress.TypePin:
	default:
		return
	}
	if o.Text == "" || !strings.Contains(o.Text, "{") {
		return
	}
	toks := tmpl.Lex(o.Text)
	chain := p.commandChain(ctx, run, tmpl.WithCondRefs(toks))
	values := chain.Plan(ctx, toks, p.logScopeFailure(run.c))
	buf := GetBuf()
	buf = chain.Render(buf, toks, values)
	o.Text = string(buf)
	PutBuf(buf)
}

// chatLines fans one expanded TypeChat body into one action per non-empty
// line, each with its own slash-verb translation, capped at
// validate.MaxResponseLines. User-controlled values are sanitizeVar'd before
// they reach here, so an embedded newline in {args} cannot mint a second line.
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

// claimedCounterValue applies one event's counter bump exactly once: a fresh
// dedup claim bumps and renders the new value; a replay (redelivered command
// line) skips the increment and renders the counter's CURRENT value via a
// peek, so the re-run line shows the same number instead of double-counting;
// a failed bump releases its claim so redelivery retries. The kill switch
// (nil dedup) degrades to the plain unguarded bump. An empty result means
// "render without a value" — a bump error or an unknown-counter peek.
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

// firstArg returns the first whitespace-delimited word of a command's
// arguments — the same word emitCommand renders as {touser}. Fields rather
// than a space Cut so a tab after the mention cannot glue itself to the name.
func firstArg(args string) string {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// gateRule is the set of checks one command is gated by, so the gate takes a
// single value rather than a long parameter list. runBaked builds it from a
// module.Command; runCustom builds it from a projection.Command.
type gateRule struct {
	name          string
	allowedUserID string
	perm          module.Role
	liveOnly      bool
	cooldown      time.Duration
}

// gate applies the one shared command gate — permission, then live-only, then
// cooldown — and returns (true, nil) only when every applicable check passes.
// Each check is its own helper so the gate reads as three linear steps and
// allocates nothing on the hot path (the cooldown key is built into a pooled
// buffer).
func (p *Pipeline) gate(ctx context.Context, c *module.Context, r gateRule) (bool, error) {
	if !permits(c, r.allowedUserID, r.perm) {
		return false, nil
	}
	if ok, err := p.liveOK(ctx, c, r.liveOnly); !ok {
		return false, err
	}
	return p.cooldownOK(ctx, c.BroadcasterID, r.name, r.cooldown)
}

// permits checks the permission tier: an explicit allowed user overrides the
// role tier entirely.
func permits(c *module.Context, allowedUserID string, perm module.Role) bool {
	if allowedUserID != "" {
		return c.Env.ChatterUserID == allowedUserID
	}
	return c.Chatter().Allows(perm)
}

// liveOK passes when the command is not live-only or the broadcaster is live.
func (p *Pipeline) liveOK(ctx context.Context, c *module.Context, liveOnly bool) (bool, error) {
	if !liveOnly {
		return true, nil
	}
	return p.live.IsLive(ctx, c.BroadcasterID)
}

// cooldownOK passes when the command has no cooldown or its window is free (and
// claims it).
func (p *Pipeline) cooldownOK(ctx context.Context, broadcasterID uint64, name string, cooldown time.Duration) (bool, error) {
	if cooldown <= 0 {
		return true, nil
	}
	return p.cooldown.Allow(ctx, cooldownKey(broadcasterID, name), cooldown)
}

// CommandCooldownKey exposes the gate's cooldown key for a module that routes a
// subcommand to the same reply as a standalone command (e.g. !queue list vs
// !list) and must share that command's throttle window rather than sidestep it.
func CommandCooldownKey(broadcasterID uint64, name string) string {
	return cooldownKey(broadcasterID, name)
}

// cooldownKey builds "cooldown:cmd:<broadcasterID>:<name>" into a pooled scratch
// buffer, appending the id with strconv so the hot path does no fmt-style
// allocation. The buffer is returned to the pool before the string is handed off.
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
