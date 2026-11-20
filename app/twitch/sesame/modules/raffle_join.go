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

func (d drawOwner) replyRaffleJoin(ctx context.Context, c *module.Context, emit module.Emit, joined draw.JoinOutcome) {
	cfg := engine.ReadRaffleConfig(ctx, d.deps.Proj, c.BroadcasterID)
	reply := raffleJoinReplies{chatReplier: newChatReplier(c), cfg: cfg, q: d.deps.Queue}
	reply.replyJoin(ctx, emit, joined)
}

func (rc raffleJoinReplies) replyJoin(ctx context.Context, emit module.Emit, entry draw.JoinOutcome) {
	switch {
	case !entry.Open && rc.cfg.NoRaffleOn():
		rc.reply(emit, rc.cfg.NoRaffleMessage, "raffle.join.closed")
	case entry.Open && rc.entryLineOn(entry.Added):
		rc.replyEntered(ctx, emit, entry)
	}
}

func (rc raffleJoinReplies) entryLineOn(added bool) bool {
	if added {
		return rc.cfg.JoinOn()
	}
	return rc.cfg.AlreadyOn()
}

func (rc raffleJoinReplies) replyEntered(ctx context.Context, emit module.Emit, entry draw.JoinOutcome) {
	override, key := rc.cfg.AlreadyMessage, replyKey("raffle.join.already")
	if entry.Added {
		override, key = rc.cfg.JoinMessage, "raffle.joined"
	}
	line := rc.render(override, key, "count", strconv.FormatInt(entry.Count, 10))
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
