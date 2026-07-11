package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/projection"

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
// emits is routed through the post-processing middleware (see emitCommand), so a
// baked command can write "/announce ..." the same way a custom one does. num is
// the inline numeric suffix the trigger absorbed ("" when none / not a
// NumericSuffix command); it is exposed on the Context for the command to read.
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
	return cmd.Run(ctx, c, args, func(o *module.Output) { p.emitCommand(o, emit) })
}

// runCustom resolves a broadcaster's custom command, gates it with the same rule
// as a baked command, then expands its response, translates any slash-verb, and
// emits one chat line.
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
		zap.Uint64("broadcaster_id", c.BroadcasterID),
	)

	// Route the expanded response through the post-processing middleware, one
	// output per line — a multi-line response sends one chat message per line,
	// each with its own slash-verb translation. A line left with no payload (an
	// "/announce" with no text, a "/shoutout" with no target) is dropped; the
	// run counts once if anything was emitted.
	counters := p.bumpCounterTokens(ctx, c, cc.Name, cc.Response)
	emitted := p.emitResponse(c, cc.Response, args, counters, emit)
	if !emitted {
		return nil
	}

	// Count the successful run. The reporter sums ticks locally and publishes one
	// event per command per flush window, so chat spam never floods NATS. cc.Name
	// is the canonical key (an alias lookup resolves to it), so alias invocations
	// all count against the one command.
	if p.uses != nil {
		p.uses.Record(c.BroadcasterID, cc.Name)
	}

	return nil
}

// emitResponse expands a custom command's response template once, then emits
// one chat output per non-empty line through the post-processing middleware
// (each line gets its own slash-verb translation, so "/announce hi\nplain"
// mixes an announcement and a chat line). The line count is capped at
// validate.MaxResponseLines — the write path enforces it, this is the emit-side
// backstop so an expanded token can never fan out further. Reports whether at
// least one line was actually emitted.
func (p *Pipeline) emitResponse(c *module.Context, response, args string, counters map[string]string, emit module.Emit) bool {
	// {user}/{sender}/{channel} render the display name (login fallback); {touser}
	// defaults to the sender's display name and is otherwise the @mention the
	// chatter typed, taken verbatim.
	sender := c.Env.ChatterName()
	touser := sender
	if args != "" {
		firstWord, _, _ := strings.Cut(args, " ")
		touser = strings.TrimPrefix(firstWord, "@")
	}

	// Enforce the user-controlled variables: strip a leading slash run so a
	// crafted {args}/{touser} can never inject a leading slash-verb (/ban,
	// /timeout) into the expanded response for Translate to route. Command
	// CONTENT is validated at save-time on the dashboard; this guards only the
	// runtime injection vector.
	args = sanitizeVar(args)
	touser = sanitizeVar(touser)

	buf := GetBuf()
	buf = expandCommand(buf, response, tokens{
		user:     sender,
		sender:   sender,
		args:     args,
		touser:   touser,
		channel:  c.Env.BroadcasterName(),
		counters: counters,
	})
	expanded := string(buf)
	PutBuf(buf)

	emitted := false
	lines := 0
	for line := range strings.SplitSeq(expanded, "\n") {
		if line == "" {
			continue
		}
		lines++
		if lines > validate.MaxResponseLines {
			break
		}
		out := GetOutput()
		out.Type = outgress.TypeChat
		out.BroadcasterID = c.Env.BroadcasterUserID
		out.Text = line
		if p.emitCommand(out, emit) {
			emitted = true
		}
		PutOutput(out)
	}
	return emitted
}

// bumpCounterTokens resolves a response's {counter:<name>} tokens: each
// distinct counter is bumped by one — against the channel value, the sender,
// or the (sender, command) bucket, per the counter's own scope — and its new
// value is returned for expansion. command is the canonical name of the
// custom command being run, which keys a viewer+command counter's bucket. nil
// when the response references no counter or no loyalty store is wired —
// expandCommand then leaves the token visible, matching every other unknown
// token. A bump failure renders the counter without a value rather than
// blocking the reply.
func (p *Pipeline) bumpCounterTokens(ctx context.Context, c *module.Context, command, response string) map[string]string {
	if p.loyalty == nil || !strings.Contains(response, "{"+counterTokenPrefix) {
		return nil
	}
	names := counterTokenNames(response)
	if len(names) == 0 {
		return nil
	}
	viewerID, _ := strconv.ParseUint(c.Env.ChatterUserID, 10, 64)
	counters := make(map[string]string, len(names))
	for _, name := range names {
		value, err := p.loyalty.CounterBump(ctx, c.BroadcasterID, name, viewerID, command, 1)
		if err != nil {
			p.log.Warn("counter token bump failed",
				zap.Uint64("broadcaster_id", c.BroadcasterID),
				zap.String("counter", name),
				zap.Error(err),
			)
			continue
		}
		counters[name] = strconv.FormatInt(value, 10)
	}
	return counters
}

// emitCommand is the command-output post-processing middleware. Every output a
// command produces (baked or custom) passes through it before publish: Translate
// routes a leading slash-verb to the right outgress action (/announce -> announce
// with color, /shoutout -> shoutout; a plain line or /me stays chat), and an
// action left empty (an /announce with no text, a /shoutout with no target) is
// dropped so Twitch is never sent a call it would reject. It is deliberately not
// gated: routing an output is not a privilege, so permission is the job of the
// command that produced the output, not of the announce/shoutout verb. It
// reports whether the output was actually emitted.
func (p *Pipeline) emitCommand(o *module.Output, emit module.Emit) bool {
	Translate(o)
	if isEmptyAction(o) {
		return false
	}
	emit(o)
	return true
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
