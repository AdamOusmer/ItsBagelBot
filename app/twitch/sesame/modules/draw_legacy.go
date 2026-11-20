// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"

	"go.uber.org/zap"
)

type raffleCmd struct {
	chatReplier
	r   engine.RaffleStore
	q   engine.QueueStore
	cfg engine.LegacyRaffleConfig
	log *zap.Logger
}

func newRaffleCmd(d engine.Deps, c *module.Context, log *zap.Logger) (rc raffleCmd, ok bool) {
	if d.Raffle == nil {
		return raffleCmd{}, false
	}
	rc = raffleCmd{chatReplier: newChatReplier(c), r: d.Raffle, q: d.Queue, log: log}
	return rc, true
}

func (rc raffleCmd) entrant() engine.RaffleEntrant {
	return engine.RaffleEntrant{ID: rc.c.Env.ChatterUserID, Login: strings.ToLower(rc.c.Env.ChatterUserLogin)}
}

func (rc raffleCmd) join(ctx context.Context, _ string, emit module.Emit) error {
	entrant := rc.entrant()
	if entrant.ID == "" || entrant.Login == "" {
		return nil
	}
	entry, err := rc.r.Join(ctx, rc.c.BroadcasterID, entrant)
	if err != nil {
		return err
	}
	rc.raffleJoinReplies().replyJoin(ctx, emit, entry)
	return nil
}

func (rc raffleCmd) last(res *engine.RaffleResult, emit module.Emit) {
	switch {
	case res == nil:
		rc.reply(emit, "", "raffle.last.none")
	case len(res.Winners) == 0:
		rc.reply(emit, "", "raffle.last.empty")
	case len(res.Claims) > 0:
		rc.reply(emit, "", "raffle.last.confirmed",
			"targets", mentionTargets(res.Winners),
			"entrants", strconv.FormatInt(res.Entrants, 10),
			"confirmed", strconv.Itoa(len(res.Claims)),
			"total", strconv.Itoa(len(res.Winners)))
	default:
		rc.reply(emit, "", "raffle.last",
			"targets", mentionTargets(res.Winners),
			"entrants", strconv.FormatInt(res.Entrants, 10))
	}
}

func (rc raffleCmd) claimConfirm(ctx context.Context, emit module.Emit) error {
	viewerID := rc.c.Env.ChatterUserID
	if viewerID == "" || !rc.cfg.ClaimOn() {
		return nil
	}
	outcome, err := rc.r.Claim(ctx, rc.c.BroadcasterID, viewerID)
	if err != nil {
		rc.log.Warn("raffle: claim failed", rc.c.BID(), zap.Error(err))
		return err
	}
	switch outcome {
	case engine.ClaimOk:
		if rc.cfg.ClaimOkOn() {
			rc.reply(emit, rc.cfg.ClaimOkMessage, "raffle.claim.ok")
		}
	case engine.ClaimAlready:
		rc.reply(emit, "", "raffle.claim.already")
	case engine.ClaimLate:
		rc.reply(emit, "", "raffle.claim.late")
	default:
		rc.reply(emit, "", "raffle.claim.none")
	}
	return nil
}

func mentionTargets(winners []engine.RaffleWinner) string {
	prefixed := make([]string, len(winners))
	for i, w := range winners {
		prefixed[i] = "@" + w.Login
	}
	return strings.Join(prefixed, ", ")
}

func (d drawOwner) legacy(ctx context.Context, c *module.Context) (raffleCmd, bool) {
	rc, ok := newRaffleCmd(d.deps, c, d.log)
	if ok {
		rc.cfg = engine.ReadLegacyRaffleConfig(ctx, d.deps.Proj, c.BroadcasterID)
	}
	return rc, ok
}

func (d drawOwner) legacyResult(ctx context.Context, c *module.Context) (*engine.RaffleResult, bool, error) {
	if d.deps.Raffle == nil {
		return nil, false, nil
	}
	return d.deps.Raffle.LastResult(ctx, c.BroadcasterID)
}

func (d drawOwner) legacyClaim(ctx context.Context, c *module.Context) (raffleCmd, bool, error) {
	rc, exists := d.legacy(ctx, c)
	if !exists || !rc.cfg.ClaimOn() {
		return rc, false, nil
	}
	_, found, err := d.legacyResult(ctx, c)
	return rc, found, err
}

func (d drawOwner) legacyWinner(ctx context.Context, c *module.Context, emit module.Emit, state drawCommandState) error {
	giveawayOn := c.ModuleOn(engine.GiveawayModuleName)
	if replied, err := d.lastGiveaway(ctx, c, emit, giveawayOn); replied || err != nil {
		return err
	}
	if rc, ok := d.legacy(ctx, c); ok && wantsLegacyWinner(c, state.legacyFound) {
		rc.last(state.result, emit)
		return nil
	}
	if giveawayOn {
		newChatReplier(c).reply(emit, "", "draw.status.none")
	}
	return nil
}

func (d drawOwner) lastGiveaway(ctx context.Context, c *module.Context, emit module.Emit, enabled bool) (bool, error) {
	if !enabled {
		return false, nil
	}
	rows, err := engine.LastGiveawayWinners(ctx, d.deps.GiveawayWinners, c.BroadcasterID)
	if err != nil || len(rows) == 0 {
		return false, err
	}
	reply := newChatReplier(c)
	for _, line := range engine.GiveawayHistoryLines(c.Locale, rows) {
		reply.say(emit, line)
	}
	return true, nil
}

func wantsLegacyWinner(c *module.Context, found bool) bool {
	return found || c.ModuleOn(raffleModuleName)
}
