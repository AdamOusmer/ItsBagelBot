package modules

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/i18n"
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
	m.Command("queue").Everyone().Run(queueRun(d, log))
	m.Command("join").Everyone().Run(queueJoinRun(d, log))
	m.Command("leave").Everyone().Run(queueLeaveRun(d, log))
	m.Command("list").Everyone().Cooldown(queueListCooldown).Aliases("queuelist").Run(queueListRun(d, log))
	return m.Build()
}

// queueRun dispatches the !queue subcommands. The engine's command gate runs
// it for everyone; the moderator-only subcommands re-check the role here.
func queueRun(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if d.Queue == nil {
			return nil
		}
		var cfg queueConfig
		_ = c.Decode(&cfg)
		sub, rest := splitFirst(args)
		isMod := c.Chatter().Allows(module.RoleModerator)

		switch strings.ToLower(sub) {
		case "":
			return queueStatus(ctx, c, d, emit)
		case "open":
			if !isMod {
				return nil
			}
			return queueSetOpen(ctx, c, d, cfg, emit, log, true)
		case "close":
			if !isMod {
				return nil
			}
			return queueSetOpen(ctx, c, d, cfg, emit, log, false)
		case "next":
			if !isMod {
				return nil
			}
			return queueNext(ctx, c, d, cfg, emit, log)
		case "remove":
			if !isMod {
				return nil
			}
			return queueRemove(ctx, c, d, rest, emit, log)
		case "clear":
			if !isMod {
				return nil
			}
			if err := d.Queue.Clear(ctx, c.BroadcasterID); err != nil {
				log.Warn("queue: clear failed", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
				return err
			}
			queueReply(c, emit, "", "queue.cleared")
			return nil
		case "join":
			return queueJoin(ctx, c, d, cfg, emit, log)
		case "leave":
			return queueLeave(ctx, c, d, cfg, emit, log)
		case "list":
			return queueList(ctx, c, d, emit, log)
		default:
			queueReply(c, emit, "", "queue.err.usage")
			return nil
		}
	}
}

func queueJoinRun(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		if d.Queue == nil {
			return nil
		}
		var cfg queueConfig
		_ = c.Decode(&cfg)
		return queueJoin(ctx, c, d, cfg, emit, log)
	}
}

func queueLeaveRun(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		if d.Queue == nil {
			return nil
		}
		var cfg queueConfig
		_ = c.Decode(&cfg)
		return queueLeave(ctx, c, d, cfg, emit, log)
	}
}

func queueListRun(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		if d.Queue == nil {
			return nil
		}
		return queueList(ctx, c, d, emit, log)
	}
}

// queueStatus answers a bare !queue with the open/closed state and line size.
// The status readout is fixed system text (not customizable).
func queueStatus(ctx context.Context, c *module.Context, d engine.Deps, emit module.Emit) error {
	open, err := d.Queue.IsOpen(ctx, c.BroadcasterID)
	if err != nil {
		return err
	}
	_, total, err := d.Queue.List(ctx, c.BroadcasterID, 1)
	if err != nil {
		return err
	}
	key := "queue.status.closed"
	if open {
		key = "queue.status.open"
	}
	queueReply(c, emit, "", key, "count", strconv.FormatInt(total, 10))
	return nil
}

func queueSetOpen(ctx context.Context, c *module.Context, d engine.Deps, cfg queueConfig, emit module.Emit, log *zap.Logger, open bool) error {
	if err := d.Queue.SetOpen(ctx, c.BroadcasterID, open); err != nil {
		log.Warn("queue: set-open failed", zap.Bool("open", open), zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return err
	}
	if open {
		queueReply(c, emit, cfg.OpenedMessage, "queue.opened")
	} else {
		queueReply(c, emit, cfg.ClosedMessage, "queue.closed")
	}
	return nil
}

// queueJoin puts the chatter in line when the queue is open. Joining twice
// keeps the original spot and answers with it.
func queueJoin(ctx context.Context, c *module.Context, d engine.Deps, cfg queueConfig, emit module.Emit, log *zap.Logger) error {
	login := strings.ToLower(c.Env.ChatterUserLogin)
	if login == "" {
		return nil
	}
	open, err := d.Queue.IsOpen(ctx, c.BroadcasterID)
	if err != nil {
		log.Warn("queue: open check failed", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return err
	}
	if !open {
		// The "queue is closed" line is a fixed system message.
		queueReply(c, emit, "", "queue.join.closed")
		return nil
	}
	pos, _, joined, err := d.Queue.Join(ctx, c.BroadcasterID, login)
	if err != nil {
		log.Warn("queue: join failed", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return err
	}
	posStr := strconv.FormatInt(pos, 10)
	if joined {
		queueReply(c, emit, cfg.JoinMessage, "queue.join.ok", "pos", posStr)
	} else {
		queueReply(c, emit, cfg.AlreadyMessage, "queue.join.already", "pos", posStr)
	}
	return nil
}

func queueLeave(ctx context.Context, c *module.Context, d engine.Deps, cfg queueConfig, emit module.Emit, log *zap.Logger) error {
	login := strings.ToLower(c.Env.ChatterUserLogin)
	if login == "" {
		return nil
	}
	removed, err := d.Queue.Remove(ctx, c.BroadcasterID, login)
	if err != nil {
		log.Warn("queue: leave failed", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return err
	}
	if removed {
		queueReply(c, emit, cfg.LeaveMessage, "queue.leave.ok")
	} else {
		// The "you are not in the queue" line is a fixed system message.
		queueReply(c, emit, "", "queue.leave.not_in")
	}
	return nil
}

// queueNext pulls the front of the line and announces them to chat.
func queueNext(ctx context.Context, c *module.Context, d engine.Deps, cfg queueConfig, emit module.Emit, log *zap.Logger) error {
	login, remaining, err := d.Queue.Pop(ctx, c.BroadcasterID)
	if err != nil {
		log.Warn("queue: next failed", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return err
	}
	if login == "" {
		queueReply(c, emit, "", "queue.next.empty")
		return nil
	}
	queueReply(c, emit, cfg.NextMessage, "queue.next", "target", login, "count", strconv.FormatInt(remaining, 10))
	return nil
}

// queueRemove takes a named viewer out of the line ("!queue remove @user"). The
// confirmation is fixed system text.
func queueRemove(ctx context.Context, c *module.Context, d engine.Deps, args string, emit module.Emit, log *zap.Logger) error {
	target, _ := splitFirst(args)
	target = strings.ToLower(strings.TrimPrefix(target, "@"))
	if target == "" {
		queueReply(c, emit, "", "queue.remove.usage")
		return nil
	}
	removed, err := d.Queue.Remove(ctx, c.BroadcasterID, target)
	if err != nil {
		log.Warn("queue: remove failed", zap.String("target", target), zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return err
	}
	if removed {
		queueReply(c, emit, "", "queue.remove.ok", "target", target)
	} else {
		queueReply(c, emit, "", "queue.remove.not_found", "target", target)
	}
	return nil
}

// queueList shows the next queueListLen players in order, with a "+N more"
// tail when the line is longer. The roster is fixed system text: it is a
// structured list, not a free-form template.
func queueList(ctx context.Context, c *module.Context, d engine.Deps, emit module.Emit, log *zap.Logger) error {
	entries, total, err := d.Queue.List(ctx, c.BroadcasterID, queueListLen)
	if err != nil {
		log.Warn("queue: list failed", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return err
	}
	if total == 0 {
		queueReply(c, emit, "", "queue.list.empty")
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
		queueReply(c, emit, "", "queue.list.more", "list", b.String(), "count", strconv.FormatInt(more, 10))
	} else {
		queueReply(c, emit, "", "queue.list", "list", b.String())
	}
	return nil
}

// queueReply emits one chat line. override is the broadcaster's customized
// template for this reply ("" for the fixed system lines, or an uncustomized
// customizable one); when empty the localized default for key is used. kv are
// {token},value pairs (token names without braces); {user} (the invoking
// chatter) and the generic dynamic vars ({random}, {choice:…}) are always
// available, so a customized template can use them too.
func queueReply(c *module.Context, emit module.Emit, override, key string, kv ...string) {
	tmpl := override
	if tmpl == "" {
		tmpl = i18n.T(c.Locale, key)
	}
	text := module.ExpandString(tmpl, func(k string) (string, bool) {
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i] == k {
				return kv[i+1], true
			}
		}
		if k == "user" {
			return c.Env.ChatterUserLogin, true
		}
		return module.ParseDynamic(k)
	})
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: c.Env.BroadcasterUserID,
		Text:          text,
	})
}
