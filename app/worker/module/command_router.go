package module

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/ThreeDotsLabs/watermill/message"

	"go.uber.org/zap"
)

// CommandRouter is the core module that owns ALL command dispatch. On every chat
// line it parses the "!command", looks it up first in the registry's baked
// command index and then in the broadcaster's custom commands, applies the one
// shared gate (permission, live-only, cooldown), and runs the winner. Baked
// commands run their own Run; custom commands expand their stored response,
// translate any slash-verb, and emit one chat line.
//
// It is a core module (Name() == ""): always on, immutable, unlisted. Individual
// commands, not the router, are what a broadcaster toggles.
type CommandRouter struct {
	reg      *Registry
	proj     projection.Reader
	live     IsLiveChecker
	cooldown CooldownStore
	uses     *useReporter
	log      *zap.Logger
}

// NewCommandRouter builds a router. The registry is wired in separately via Bind
// because the router is itself a module that goes into the registry, so the
// registry does not exist yet at construction time. pub carries the summed
// data.commands.used counter events (see useReporter); nil disables them.
func NewCommandRouter(proj projection.Reader, live IsLiveChecker, cooldown CooldownStore, pub message.Publisher, log *zap.Logger) *CommandRouter {
	r := &CommandRouter{proj: proj, live: live, cooldown: cooldown, log: log}
	if pub != nil {
		r.uses = newUseReporter(pub, log)
	}
	return r
}

// Close flushes and stops the use reporter. Safe on a router built without a
// publisher (tests).
func (r *CommandRouter) Close() {
	if r.uses != nil {
		r.uses.Close()
	}
}

// Bind wires the registry into the router. main constructs the router, passes it
// to NewRegistry alongside the other modules, then calls Bind(reg) with the
// resulting registry before any traffic flows. There is no init-order footgun:
// the router reads reg lazily at Handle time, so the only requirement is that
// Bind has run before the first message, which the startup sequence guarantees.
func (r *CommandRouter) Bind(reg *Registry) { r.reg = reg }

func (r *CommandRouter) Name() string        { return "" } // core: always on
func (r *CommandRouter) Events() []string    { return []string{"channel.chat.message"} }
func (r *CommandRouter) Commands() []Command { return nil }

func (r *CommandRouter) Handle(ctx context.Context, c *Context, emit Emit) error {
	name, args, ok := parseCommand(c.Env.Text)
	if !ok {
		return nil
	}

	if cmd, isBaked := r.reg.Command(name); isBaked {
		return r.runBaked(ctx, c, cmd, args, emit)
	}
	return r.runCustom(ctx, c, name, args, emit)
}

// runBaked gates and runs a command a module owns.
func (r *CommandRouter) runBaked(ctx context.Context, c *Context, cmd Command, args string, emit Emit) error {
	pass, err := r.gate(ctx, c, cmd.Name, cmd.AllowedUserID, cmd.Perm, cmd.LiveOnly, cmd.Cooldown)
	if err != nil || !pass {
		return err
	}
	return cmd.Run(ctx, c, args, emit)
}

// runCustom resolves a broadcaster's custom command, gates it with the same rule
// as a baked command, then expands its response, translates any slash-verb, and
// emits one chat line.
func (r *CommandRouter) runCustom(ctx context.Context, c *Context, name, args string, emit Emit) error {
	cc, found, err := r.proj.Command(ctx, c.BroadcasterID, name)
	if err != nil || !found || !cc.IsActive {
		return err
	}
	if cc.Response == "" {
		return nil
	}

	pass, err := r.gate(ctx, c, name, cc.AllowedUserID, ParsePerm(cc.Perm), cc.StreamOnlineOnly, time.Duration(cc.Cooldown)*time.Second)
	if err != nil || !pass {
		return err
	}

	r.log.Debug("command matched",
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

	Translate(out)


	// Skip an action Translate left with no payload (e.g. "/announce" with no
	// text, "/shoutout" with no target): Twitch would reject it and the Helix
	// call would be wasted.
	if isEmptyAction(out) {
		PutOutput(out)
		return nil
	}

	emit(out)
	PutOutput(out)

	// Count the successful run. The reporter sums ticks locally and publishes
	// one event per command per flush window (the bus rate limiter), so chat
	// spam never floods NATS. cc.Name is the canonical key (an alias lookup
	// resolves to it), so alias invocations all count against the one command.
	if r.uses != nil {
		r.uses.Record(c.BroadcasterID, cc.Name)
	}

	return nil
}

// gate applies the one shared command gate: permission (an explicit allowed user
// overrides the role tier entirely), then live-only, then cooldown. It returns
// (true, nil) only when every applicable check passes. It is pure aside from the
// store reads and allocates nothing on the hot path (the cooldown key is built
// into a pooled buffer).
func (r *CommandRouter) gate(ctx context.Context, c *Context, name, allowedUserID string, perm Role, liveOnly bool, cooldown time.Duration) (bool, error) {
	// Permission: an explicit allowed user overrides the role tier entirely.
	if allowedUserID != "" {
		if c.Env.ChatterUserID != allowedUserID {
			return false, nil
		}
	} else if !c.Chatter().Allows(perm) {
		return false, nil
	}

	if liveOnly {
		live, err := r.live.IsLive(ctx, c.BroadcasterID)
		if err != nil {
			return false, err
		}
		if !live {
			return false, nil
		}
	}

	if cooldown > 0 {
		key := cooldownKey(c.BroadcasterID, name)
		allowed, err := r.cooldown.Allow(ctx, key, cooldown)
		if err != nil {
			return false, err
		}
		if !allowed {
			return false, nil
		}
	}

	return true, nil
}

// cooldownKey builds the shared cooldown key "cooldown:cmd:<broadcasterID>:<name>"
// into a pooled scratch buffer, appending the id with strconv so the hot path
// does no fmt-style allocation. The buffer is returned to the pool before the
// string is handed off.
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
