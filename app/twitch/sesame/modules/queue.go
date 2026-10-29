// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"

	"go.uber.org/zap"
)

const queueModuleName = "queue"

const queueListLen = 10

const queueListCooldown = 5 * time.Second

type queueConfig struct {
	JoinMessage    string `json:"joinMessage"`
	AlreadyMessage string `json:"alreadyMessage"`
	LeaveMessage   string `json:"leaveMessage"`
	NextMessage    string `json:"nextMessage"`
	OpenedMessage  string `json:"openedMessage"`
	ClosedMessage  string `json:"closedMessage"`
}

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

type queueCmd struct {
	chatReplier
	q   engine.QueueStore
	cfg queueConfig
	log *zap.Logger
}

func newQueueCmd(d engine.Deps, c *module.Context, log *zap.Logger) (qc queueCmd, ok bool) {
	if d.Queue == nil {
		return queueCmd{}, false
	}
	qc = queueCmd{chatReplier: newChatReplier(c), q: d.Queue, log: log}
	_ = c.Decode(&qc.cfg)
	return qc, true
}

func queueStandalone(d engine.Deps, log *zap.Logger, fn func(queueCmd, context.Context, module.Emit) error) module.RunFunc {
	return func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		qc, ok := newQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		return fn(qc, ctx, emit)
	}
}

func queueDispatch(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		qc, ok := newQueueCmd(d, c, log)
		if !ok {
			return nil
		}
		sub, rest := splitFirst(args)
		h, known := queueSubcommands(d)[strings.ToLower(sub)]
		if !known {
			qc.reply(emit, "", "queue.err.usage")
			return nil
		}
		if h.mod && !c.Chatter().Allows(module.RoleModerator) {
			return nil
		}
		return h.run(qc, ctx, rest, emit)
	}
}

type queueSub struct {
	mod bool
	run func(qc queueCmd, ctx context.Context, rest string, emit module.Emit) error
}

func queueSubcommands(d engine.Deps) map[string]queueSub {
	return map[string]queueSub{
		"": {run: func(qc queueCmd, ctx context.Context, _ string, emit module.Emit) error { return qc.status(ctx, emit) }},
		"open": {mod: true, run: func(qc queueCmd, ctx context.Context, _ string, emit module.Emit) error {
			return qc.setOpen(ctx, emit, true)
		}},
		"close": {mod: true, run: func(qc queueCmd, ctx context.Context, _ string, emit module.Emit) error {
			return qc.setOpen(ctx, emit, false)
		}},
		"next": {mod: true, run: func(qc queueCmd, ctx context.Context, _ string, emit module.Emit) error { return qc.next(ctx, emit) }},
		"remove": {mod: true, run: func(qc queueCmd, ctx context.Context, rest string, emit module.Emit) error {
			return qc.remove(ctx, rest, emit)
		}},
		"clear": {mod: true, run: func(qc queueCmd, ctx context.Context, _ string, emit module.Emit) error { return qc.clear(ctx, emit) }},
		"join":  {run: func(qc queueCmd, ctx context.Context, _ string, emit module.Emit) error { return qc.join(ctx, emit) }},
		"leave": {run: func(qc queueCmd, ctx context.Context, _ string, emit module.Emit) error { return qc.leave(ctx, emit) }},
		"list": {run: func(qc queueCmd, ctx context.Context, _ string, emit module.Emit) error {
			return qc.cooled(ctx, d.Cooldown, "list", emit, qc.list)
		}},
		"full": {run: func(qc queueCmd, ctx context.Context, _ string, emit module.Emit) error {
			return qc.cooled(ctx, d.Cooldown, "queue_full", emit, func(ctx context.Context, emit module.Emit) error {
				return qc.full(ctx, d, emit)
			})
		}},
	}
}

func (qc queueCmd) status(ctx context.Context, emit module.Emit) error {
	open, err := qc.q.IsOpen(ctx, qc.c.BroadcasterID)
	if err != nil {
		return err
	}
	_, total, err := qc.q.List(ctx, qc.c.BroadcasterID, 1)
	if err != nil {
		return err
	}
	key := replyKey("queue.status.closed")
	if open {
		key = "queue.status.open"
	}
	qc.reply(emit, "", key, "count", strconv.FormatInt(total, 10))
	return nil
}

func (qc queueCmd) setOpen(ctx context.Context, emit module.Emit, open bool) error {
	if err := qc.q.SetOpen(ctx, qc.c.BroadcasterID, open); err != nil {
		qc.log.Warn("queue: set-open failed", zap.Bool("open", open), qc.c.BID(), zap.Error(err))
		return err
	}
	if open {
		qc.reply(emit, qc.cfg.OpenedMessage, "queue.opened")
	} else {
		qc.reply(emit, qc.cfg.ClosedMessage, "queue.closed")
	}
	return nil
}

func (qc queueCmd) join(ctx context.Context, emit module.Emit) error {
	login := strings.ToLower(qc.c.Env.ChatterUserLogin)
	if login == "" {
		return nil
	}
	open, err := qc.q.IsOpen(ctx, qc.c.BroadcasterID)
	if err != nil {
		qc.log.Warn("queue: open check failed", qc.c.BID(), zap.Error(err))
		return err
	}
	if !open {
		qc.reply(emit, "", "queue.join.closed")
		return nil
	}
	pos, _, joined, err := qc.q.Join(ctx, qc.c.BroadcasterID, login)
	if err != nil {
		qc.log.Warn("queue: join failed", qc.c.BID(), zap.Error(err))
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
		qc.log.Warn("queue: leave failed", qc.c.BID(), zap.Error(err))
		return err
	}
	if removed {
		qc.reply(emit, qc.cfg.LeaveMessage, "queue.leave.ok")
	} else {
		qc.reply(emit, "", "queue.leave.not_in")
	}
	return nil
}

func (qc queueCmd) next(ctx context.Context, emit module.Emit) error {
	login, remaining, err := qc.q.Pop(ctx, qc.c.BroadcasterID)
	if err != nil {
		qc.log.Warn("queue: next failed", qc.c.BID(), zap.Error(err))
		return err
	}
	if login == "" {
		qc.reply(emit, "", "queue.next.empty")
		return nil
	}
	qc.reply(emit, qc.cfg.NextMessage, "queue.next", "target", login, "count", strconv.FormatInt(remaining, 10))
	return nil
}

func (qc queueCmd) remove(ctx context.Context, args string, emit module.Emit) error {
	target, _ := splitFirst(args)
	target = strings.ToLower(strings.TrimPrefix(target, "@"))
	if target == "" {
		qc.reply(emit, "", "queue.remove.usage")
		return nil
	}
	removed, err := qc.q.Remove(ctx, qc.c.BroadcasterID, target)
	if err != nil {
		qc.log.Warn("queue: remove failed", zap.String("target", target), qc.c.BID(), zap.Error(err))
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
		qc.log.Warn("queue: clear failed", qc.c.BID(), zap.Error(err))
		return err
	}
	qc.reply(emit, "", "queue.cleared")
	return nil
}

func (qc queueCmd) cooled(ctx context.Context, cd engine.CooldownStore, name string, emit module.Emit, run func(context.Context, module.Emit) error) error {
	if cd != nil {
		ok, err := cd.Allow(ctx, engine.CommandCooldownKey(qc.c.BroadcasterID, name), queueListCooldown)
		if err != nil || !ok {
			return err
		}
	}
	return run(ctx, emit)
}

func (qc queueCmd) full(ctx context.Context, d engine.Deps, emit module.Emit) error {
	link, _ := publicPageURL(ctx, qc.c, d, publicPage{
		base:        d.QueueBaseURL,
		defaultBase: "https://queue.itsbagelbot.com",
		path:        "/",
		hidden:      func(projection.User) bool { return false },
	})
	qc.reply(emit, "", "queue.full", "url", link)
	return nil
}

func (qc queueCmd) list(ctx context.Context, emit module.Emit) error {
	entries, total, err := qc.q.List(ctx, qc.c.BroadcasterID, queueListLen)
	if err != nil {
		qc.log.Warn("queue: list failed", qc.c.BID(), zap.Error(err))
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
