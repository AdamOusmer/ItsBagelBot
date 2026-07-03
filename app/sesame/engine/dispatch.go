package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
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
// A baked command is first gated by its owning module's enable/premium state
// (the same enabled() check event handlers pass), so a command on a disabled or
// premium-only module never runs; that gate also wires the module's config into
// the Context before the command runs. views is the broadcaster's ModuleView set
// (nil when the chat path needs none, i.e. only core command owners).
func (p *Pipeline) dispatchCommand(ctx context.Context, c *module.Context, views map[string]projection.ModuleView, emit module.Emit) error {
	name, args, ok := parseCommand(c.Env.Text)
	if !ok {
		return nil
	}
	if bc, isBaked := p.registry.Command(name); isBaked {
		if !p.enabled(bc.Owner, views, c) {
			return nil
		}
		return p.runBaked(ctx, c, bc.Cmd, args, emit)
	}
	return p.runCustom(ctx, c, name, args, emit)
}

// runBaked gates and runs a command a module owns. Every output the command
// emits is routed through the post-processing middleware (see emitCommand), so a
// baked command can write "/announce ..." the same way a custom one does.
func (p *Pipeline) runBaked(ctx context.Context, c *module.Context, cmd module.Command, args string, emit module.Emit) error {
	pass, err := p.gate(ctx, c, cmd.Name, cmd.AllowedUserID, cmd.Perm, cmd.LiveOnly, cmd.Cooldown)
	if err != nil || !pass {
		return err
	}
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

	pass, err := p.gate(ctx, c, name, cc.AllowedUserID, module.ParsePerm(cc.Perm), cc.StreamOnlineOnly, time.Duration(cc.Cooldown)*time.Second)
	if err != nil || !pass {
		return err
	}

	p.log.Debug("command matched",
		zap.String("command", name),
		zap.String("regress", c.Regress.String()),
		zap.Uint64("broadcaster_id", c.BroadcasterID),
	)

	sender := c.Env.ChatterUserLogin
	touser := sender
	if args != "" {
		firstWord, _, _ := strings.Cut(args, " ")
		touser = strings.TrimPrefix(firstWord, "@")
	}

	buf := GetBuf()
	buf = expandCommand(buf, cc.Response, sender, sender, args, touser)
	out := GetOutput()
	out.Type = outgress.TypeChat
	out.BroadcasterID = c.Env.BroadcasterUserID
	out.Text = string(buf)
	PutBuf(buf)

	// Route the expanded response through the post-processing middleware. A
	// response left with no payload (an "/announce" with no text, a "/shoutout"
	// with no target) is dropped and not counted.
	emitted := p.emitCommand(out, emit)
	PutOutput(out)
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

// gate applies the one shared command gate: permission (an explicit allowed user
// overrides the role tier entirely), then live-only, then cooldown. It returns
// (true, nil) only when every applicable check passes. It allocates nothing on
// the hot path (the cooldown key is built into a pooled buffer).
func (p *Pipeline) gate(ctx context.Context, c *module.Context, name, allowedUserID string, perm module.Role, liveOnly bool, cooldown time.Duration) (bool, error) {
	// Permission: an explicit allowed user overrides the role tier entirely.
	if allowedUserID != "" {
		if c.Env.ChatterUserID != allowedUserID {
			return false, nil
		}
	} else if !c.Chatter().Allows(perm) {
		return false, nil
	}

	if liveOnly {
		live, err := p.live.IsLive(ctx, c.BroadcasterID)
		if err != nil {
			return false, err
		}
		if !live {
			return false, nil
		}
	}

	if cooldown > 0 {
		key := cooldownKey(c.BroadcasterID, name)
		allowed, err := p.cooldown.Allow(ctx, key, cooldown)
		if err != nil {
			return false, err
		}
		if !allowed {
			return false, nil
		}
	}

	return true, nil
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
