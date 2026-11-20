// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/module"
)

type raffleJoinReplies struct {
	chatReplier
	cfg engine.RaffleModuleConfig
	q   engine.QueueStore
}

func (rc raffleCmd) raffleJoinReplies() raffleJoinReplies {
	return raffleJoinReplies{chatReplier: rc.chatReplier, cfg: rc.cfg.RaffleModuleConfig, q: rc.q}
}

func (d drawOwner) replyRaffleJoin(ctx context.Context, c *module.Context, emit module.Emit, joined draw.JoinOutcome) {
	cfg := engine.ReadRaffleConfig(ctx, d.deps.Proj, c.BroadcasterID)
	reply := raffleJoinReplies{chatReplier: newChatReplier(c), cfg: cfg, q: d.deps.Queue}
	reply.replyJoin(ctx, emit, engine.RaffleEntry{Open: joined.Open, Joined: joined.Added, Entrants: joined.Count})
}

func (rc raffleJoinReplies) replyJoin(ctx context.Context, emit module.Emit, entry engine.RaffleEntry) {
	switch {
	case !entry.Open:
		if rc.cfg.NoRaffleOn() {
			rc.reply(emit, rc.cfg.NoRaffleMessage, "raffle.join.closed")
		}
	case rc.entryLineOn(entry):
		rc.replyEntered(ctx, emit, entry)
	}
}

func (rc raffleJoinReplies) entryLineOn(entry engine.RaffleEntry) bool {
	if entry.Joined {
		return rc.cfg.JoinOn()
	}
	return rc.cfg.AlreadyOn()
}

func (rc raffleJoinReplies) replyEntered(ctx context.Context, emit module.Emit, entry engine.RaffleEntry) {
	override, key := rc.cfg.AlreadyMessage, replyKey("raffle.join.already")
	if entry.Joined {
		override, key = rc.cfg.JoinMessage, "raffle.joined"
	}
	line := rc.render(override, key, "count", strconv.FormatInt(entry.Entrants, 10))
	if override == "" && rc.queueOpen(ctx) {
		line += " " + rc.render("", "raffle.join.queue_hint")
	}
	rc.say(emit, line)
}

func (rc raffleJoinReplies) queueOpen(ctx context.Context) bool {
	if rc.q == nil || !rc.c.ModuleOn(queueModuleName) {
		return false
	}
	open, err := rc.q.IsOpen(ctx, rc.c.BroadcasterID)
	return err == nil && open
}
