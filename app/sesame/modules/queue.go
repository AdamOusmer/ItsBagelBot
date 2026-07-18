package modules

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// queueModuleName is the ModuleView key; the console MODULE_CATALOG entry and
// the dashboard module page use the same id.
const queueModuleName = "queue"

// queueListLen is how many waiting players !list shows; anything past it is
// summarized as a "+N more" tail so a long line never floods chat.
const queueListLen = 10

// queueListCooldown throttles the chat-facing list reply per channel. The
// queue mutations themselves are single cheap valkey ops and stay uncooled —
// a shared window on !join would drop other viewers' joins in a burst.
const queueListCooldown = 5 * time.Second

// queueConfig holds the broadcaster's customized reply templates. Each field is
// a dashboard-editable message; empty falls back to that reply's localized
// default (see i18n, keyed by the constant next to each field). Only the
// conversational replies are customizable — the roster (!list), the status
// readout, the moderator-action confirmations and the usage/error lines stay
// fixed system text, so their keys have no field here.
type queueConfig struct {
	JoinMessage    string `json:"joinMessage"`    // i18n queue.join.ok       {user} {pos}
	AlreadyMessage string `json:"alreadyMessage"` // i18n queue.join.already  {user} {pos}
	LeaveMessage   string `json:"leaveMessage"`   // i18n queue.leave.ok      {user}
	NextMessage    string `json:"nextMessage"`    // i18n queue.next          {target} {count}
	OpenedMessage  string `json:"openedMessage"`  // i18n queue.opened
	ClosedMessage  string `json:"closedMessage"`  // i18n queue.closed
}

// Queue owns the viewer play queue: a line viewers join to play with the
// streamer. It is a named, opt-in module (KindOptIn): off by default, enabled
// on the dashboard. Because a disabled module's triggers fall through to
// custom commands, the friendly !join / !list names cost nothing on channels
// that never enable it.
//
//	!queue                  → status (open/closed, how many waiting)
//	!queue open|close       → mod: accept / stop accepting joins (line kept)
//	!queue next             → mod: pull the next player and announce them
//	!queue remove <user>    → mod: take someone out of the line
//	!queue clear            → mod: empty the line
//	!queue leave / !leave   → step out of the line
//	!join                   → get in line (also !queue join)
//	!list                   → show the next 10 (also !queue list, !queuelist)
//
// The conversational replies (join, next, open/close announcements, ...) are
// customizable per broadcaster on the dashboard; the roster and system lines
// are fixed. Moderator subcommands typed by a non-mod are silently ignored,
// matching the engine gate's silence on an insufficient role.
func Queue(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(queueModuleName, module.KindOptIn)
	m.Command("queue").Everyone().Run(queueDispatch(d, log))
	m.Command("join").Everyone().Run(queueStandalone(d, log, queueCmd.join))
	m.Command("leave").Everyone().Run(queueStandalone(d, log, queueCmd.leave))
	m.Command("list").Everyone().Cooldown(queueListCooldown).Aliases("queuelist").Run(queueStandalone(d, log, queueCmd.list))
	return m.Build()
}

// queueCmd bundles the per-invocation state every queue handler shares (the
// store, the message context, the decoded config, the logger) so each handler
// is a method taking only its own arguments instead of threading the same five
// values through every call. It is built once per command by newQueueCmd.
type queueCmd struct {
	q   engine.QueueStore
	c   *module.Context
	cfg queueConfig
	log *zap.Logger
}

// newQueueCmd assembles the shared state for one invocation, decoding the
// broadcaster's reply-template overrides. ok is false when the queue store is
// absent (the module is inert), so callers return without acting.
func newQueueCmd(d engine.Deps, c *module.Context, log *zap.Logger) (qc queueCmd, ok bool) {
	if d.Queue == nil {
		return queueCmd{}, false
	}
	qc = queueCmd{q: d.Queue, c: c, log: log}
	_ = c.Decode(&qc.cfg)
	return qc, true
}

// queueStandalone adapts a queueCmd method into a RunFunc for the argument-less
// commands (!join, !leave, !list): it builds the shared state, then delegates.
// It is the one place the "inert when the store is nil, else decode + run"
// boilerplate lives, so the three commands do not each repeat it.
func queueStandalone(d engine.Deps, log *zap.Logger, fn func(queueCmd, context.Context, module.Emit) error) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		qc, ok := newQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		return fn(qc, ctx, emit)
	}
}

// queueDispatch handles !queue and routes its subcommands. The engine's command
// gate runs it for everyone; the moderator-only subcommands re-check the role.
func queueDispatch(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		qc, ok := newQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		sub, rest := splitFirst(args)
		isMod := c.Chatter().Allows(module.RoleModerator)

		switch strings.ToLower(sub) {
		case "":
			return qc.status(ctx, emit)
		case "open":
			if !isMod {
				return nil
			}
			return qc.setOpen(ctx, emit, true)
		case "close":
			if !isMod {
				return nil
			}
			return qc.setOpen(ctx, emit, false)
		case "next":
			if !isMod {
				return nil
			}
			return qc.next(ctx, emit)
		case "remove":
			if !isMod {
				return nil
			}
			return qc.remove(ctx, rest, emit)
		case "clear":
			if !isMod {
				return nil
			}
			return qc.clear(ctx, emit)
		case "join":
			return qc.join(ctx, emit)
		case "leave":
			return qc.leave(ctx, emit)
		case "list":
			return qc.listCooled(ctx, d.Cooldown, emit)
		default:
			qc.reply(emit, "", "queue.err.usage")
			return nil
		}
	}
}

// status answers a bare !queue with the open/closed state and line size. The
// status readout is fixed system text (not customizable).
func (qc queueCmd) status(ctx context.Context, emit module.Emit) error {
	open, err := qc.q.IsOpen(ctx, qc.c.BroadcasterID)
	if err != nil {
		return err
	}
	_, total, err := qc.q.List(ctx, qc.c.BroadcasterID, 1)
	if err != nil {
		return err
	}
	key := "queue.status.closed"
	if open {
		key = "queue.status.open"
	}
	qc.reply(emit, "", key, "count", strconv.FormatInt(total, 10))
	return nil
}

func (qc queueCmd) setOpen(ctx context.Context, emit module.Emit, open bool) error {
	if err := qc.q.SetOpen(ctx, qc.c.BroadcasterID, open); err != nil {
		qc.log.Warn("queue: set-open failed", zap.Bool("open", open), qc.bid(), zap.Error(err))
		return err
	}
	if open {
		qc.reply(emit, qc.cfg.OpenedMessage, "queue.opened")
	} else {
		qc.reply(emit, qc.cfg.ClosedMessage, "queue.closed")
	}
	return nil
}

// join puts the chatter in line when the queue is open. Joining twice keeps the
// original spot and answers with it.
func (qc queueCmd) join(ctx context.Context, emit module.Emit) error {
	login := strings.ToLower(qc.c.Env.ChatterUserLogin)
	if login == "" {
		return nil
	}
	open, err := qc.q.IsOpen(ctx, qc.c.BroadcasterID)
	if err != nil {
		qc.log.Warn("queue: open check failed", qc.bid(), zap.Error(err))
		return err
	}
	if !open {
		// The "queue is closed" line is a fixed system message.
		qc.reply(emit, "", "queue.join.closed")
		return nil
	}
	pos, _, joined, err := qc.q.Join(ctx, qc.c.BroadcasterID, login)
	if err != nil {
		qc.log.Warn("queue: join failed", qc.bid(), zap.Error(err))
		return err
	}
	posStr := strconv.FormatInt(pos, 10)
	if joined {
		qc.reply(emit, qc.cfg.JoinMessage, "queue.join.ok", "pos", posStr)
	} else {
		qc.reply(emit, qc.cfg.AlreadyMessage, "queue.join.already", "pos", posStr)
	}
	return nil
}

func (qc queueCmd) leave(ctx context.Context, emit module.Emit) error {
	login := strings.ToLower(qc.c.Env.ChatterUserLogin)
	if login == "" {
		return nil
	}
	removed, err := qc.q.Remove(ctx, qc.c.BroadcasterID, login)
	if err != nil {
		qc.log.Warn("queue: leave failed", qc.bid(), zap.Error(err))
		return err
	}
	if removed {
		qc.reply(emit, qc.cfg.LeaveMessage, "queue.leave.ok")
	} else {
		// The "you are not in the queue" line is a fixed system message.
		qc.reply(emit, "", "queue.leave.not_in")
	}
	return nil
}

// next pulls the front of the line and announces them to chat.
func (qc queueCmd) next(ctx context.Context, emit module.Emit) error {
	login, remaining, err := qc.q.Pop(ctx, qc.c.BroadcasterID)
	if err != nil {
		qc.log.Warn("queue: next failed", qc.bid(), zap.Error(err))
		return err
	}
	if login == "" {
		qc.reply(emit, "", "queue.next.empty")
		return nil
	}
	qc.reply(emit, qc.cfg.NextMessage, "queue.next", "target", login, "count", strconv.FormatInt(remaining, 10))
	return nil
}

// remove takes a named viewer out of the line ("!queue remove @user"). The
// confirmation is fixed system text.
func (qc queueCmd) remove(ctx context.Context, args string, emit module.Emit) error {
	target, _ := splitFirst(args)
	target = strings.ToLower(strings.TrimPrefix(target, "@"))
	if target == "" {
		qc.reply(emit, "", "queue.remove.usage")
		return nil
	}
	removed, err := qc.q.Remove(ctx, qc.c.BroadcasterID, target)
	if err != nil {
		qc.log.Warn("queue: remove failed", zap.String("target", target), qc.bid(), zap.Error(err))
		return err
	}
	if removed {
		qc.reply(emit, "", "queue.remove.ok", "target", target)
	} else {
		qc.reply(emit, "", "queue.remove.not_found", "target", target)
	}
	return nil
}

func (qc queueCmd) clear(ctx context.Context, emit module.Emit) error {
	if err := qc.q.Clear(ctx, qc.c.BroadcasterID); err != nil {
		qc.log.Warn("queue: clear failed", qc.bid(), zap.Error(err))
		return err
	}
	qc.reply(emit, "", "queue.cleared")
	return nil
}

// listCooled applies the standalone !list's per-channel throttle to the
// !queue list route: both claim the engine's "list" cooldown key, so the two
// spellings share one queueListCooldown window instead of the subcommand
// sidestepping it. A nil store (tests) runs the roster unthrottled; a throttled
// or errored claim stays silent, matching the engine gate.
func (qc queueCmd) listCooled(ctx context.Context, cd engine.CooldownStore, emit module.Emit) error {
	if cd != nil {
		ok, err := cd.Allow(ctx, engine.CommandCooldownKey(qc.c.BroadcasterID, "list"), queueListCooldown)
		if err != nil || !ok {
			return err
		}
	}
	return qc.list(ctx, emit)
}

// list shows the next queueListLen players in order, with a "+N more" tail when
// the line is longer. The roster is fixed system text: it is a structured list,
// not a free-form template.
func (qc queueCmd) list(ctx context.Context, emit module.Emit) error {
	entries, total, err := qc.q.List(ctx, qc.c.BroadcasterID, queueListLen)
	if err != nil {
		qc.log.Warn("queue: list failed", qc.bid(), zap.Error(err))
		return err
	}
	if total == 0 {
		qc.reply(emit, "", "queue.list.empty")
		return nil
	}

	var b strings.Builder
	for i, login := range entries {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(login)
	}

	if more := total - int64(len(entries)); more > 0 {
		qc.reply(emit, "", "queue.list.more", "list", b.String(), "count", strconv.FormatInt(more, 10))
	} else {
		qc.reply(emit, "", "queue.list", "list", b.String())
	}
	return nil
}

// reply emits one chat line. override is the broadcaster's customized template
// for this reply ("" for the fixed system lines, or an uncustomized
// customizable one); when empty the localized default for key is used. kv are
// {token},value pairs (token names without braces); {user} (the invoking
// chatter) and the generic dynamic vars ({random}, {choice:…}) are always
// available, so a customized template can use them too.
func (qc queueCmd) reply(emit module.Emit, override, key string, kv ...string) {
	tmpl := override
	if tmpl == "" {
		tmpl = i18n.T(qc.c.Locale, key)
	}
	text := module.ExpandString(tmpl, func(k string) (string, bool) {
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i] == k {
				return kv[i+1], true
			}
		}
		if k == "user" {
			return qc.c.Env.ChatterUserLogin, true
		}
		return module.ParseDynamic(k)
	})
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: qc.c.Env.BroadcasterUserID,
		Text:          text,
	})
}

// bid is the broadcaster-id log field, shared by every handler's warn path.
func (qc queueCmd) bid() zap.Field { return zap.Uint64("broadcaster_id", qc.c.BroadcasterID) }
