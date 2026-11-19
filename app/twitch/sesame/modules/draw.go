// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/module"

	"go.uber.org/zap"
)

const drawModuleName = "draw"

func Draw(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	owner := drawOwner{deps: d, log: log}
	m := module.NewModule(drawModuleName, module.KindCore)
	m.Command("join").Everyone().Yields().When(owner.ownsJoin).Run(owner.join)
	m.Command("claim").Everyone().Yields().When(owner.ownsClaim).Run(owner.claim)
	m.Command("winner").Everyone().Yields().When(owner.ownsWinner).Cooldown(draw.Pins.WinnerCooldown).Run(owner.winner)
	return m.Build()
}

type drawOwner struct {
	deps engine.Deps
	log  *zap.Logger
}

func (d drawOwner) activeJoin(ctx context.Context, c *module.Context) (draw.Round, bool, error) {
	if d.deps.Draw == nil {
		return draw.Round{}, false, nil
	}
	return d.deps.Draw.Joinable(ctx, draw.ChannelID(c.BroadcasterID))
}

func (d drawOwner) ownsJoin(ctx context.Context, c *module.Context) bool {
	_, found, err := d.activeJoin(ctx, c)
	if found || err != nil {
		return true
	}
	if d.deps.Raffle == nil {
		return false
	}
	legacy, err := d.deps.Raffle.Status(ctx, c.BroadcasterID)
	return err != nil || legacy.Open || wantsClosedRaffle(c)
}

func wantsClosedRaffle(c *module.Context) bool {
	return c.ModuleOn(raffleModuleName) && !c.ModuleOn(queueModuleName)
}

func (d drawOwner) join(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
	r, found, err := d.activeJoin(ctx, c)
	if err != nil {
		return err
	}
	if found {
		return d.joinRound(ctx, c, r, emit)
	}
	if rc, ok := d.legacy(ctx, c); ok {
		return rc.join(ctx, "", emit)
	}
	return nil
}

func (d drawOwner) joinRound(ctx context.Context, c *module.Context, r draw.Round, emit module.Emit) error {
	viewer := draw.ViewerID(c.Env.ChatterUserID)
	if viewer == "" {
		return nil
	}
	joined, err := d.deps.Draw.Join(ctx, draw.JoinRequest{Channel: draw.ChannelID(c.BroadcasterID),
		Round: r.ID, Viewer: viewer, Login: strings.ToLower(c.Env.ChatterUserLogin), Weight: draw.Pins.EntryWeight})
	if err == nil && !joined.Open {
		newChatReplier(c).reply(emit, "", "draw.join.closed")
	}
	return err
}

func (d drawOwner) ownsClaim(ctx context.Context, c *module.Context) bool {
	if d.deps.Draw != nil {
		found, err := d.deps.Draw.CanClaim(ctx, draw.ChannelID(c.BroadcasterID), draw.ViewerID(c.Env.ChatterUserID))
		if found || err != nil {
			return true
		}
	}
	_, found, err := d.legacyClaim(ctx, c)
	return found || err != nil
}

func (d drawOwner) claim(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
	outcome, err := d.claimRound(ctx, c)
	if err != nil {
		return err
	}
	reply := newChatReplier(c)
	switch {
	case outcome.Claimed > 0:
		reply.reply(emit, "", "draw.claim.ok")
	case outcome.Late > 0:
		reply.reply(emit, "", "draw.claim.late")
	default:
		rc, found, err := d.legacyClaim(ctx, c)
		if err != nil || !found {
			return err
		}
		return rc.claimConfirm(ctx, emit)
	}
	return nil
}

func (d drawOwner) claimRound(ctx context.Context, c *module.Context) (draw.ClaimOutcome, error) {
	if d.deps.Draw == nil {
		return draw.ClaimOutcome{}, nil
	}
	return d.deps.Draw.Claim(ctx, draw.ChannelID(c.BroadcasterID), draw.ViewerID(c.Env.ChatterUserID))
}

func (d drawOwner) settling(ctx context.Context, c *module.Context) (draw.Round, []draw.Slot, bool, error) {
	if d.deps.Draw == nil {
		return draw.Round{}, nil, false, nil
	}
	return d.deps.Draw.Settling(ctx, draw.ChannelID(c.BroadcasterID), "")
}

func (d drawOwner) ownsWinner(ctx context.Context, c *module.Context) bool {
	_, _, found, err := d.settling(ctx, c)
	if found || err != nil {
		return true
	}
	if c.ModuleOn(raffleModuleName) || c.ModuleOn("giveaway") {
		return true
	}
	_, found, err = d.legacyResult(ctx, c)
	return found || err != nil
}

func (d drawOwner) winner(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
	r, slots, found, err := d.settling(ctx, c)
	if err != nil {
		return err
	}
	if found {
		replyDrawWinner(c, emit, r, slots)
		return nil
	}
	return d.legacyWinner(ctx, c, emit)
}

func replyDrawWinner(c *module.Context, emit module.Emit, r draw.Round, slots []draw.Slot) {
	groups := make(map[replyKey][]draw.Slot)
	for _, slot := range slots {
		if key := drawWinnerStatus(r.Terms.Kind, slot.State); key != "" {
			groups[key] = append(groups[key], slot)
		}
	}
	reply := newChatReplier(c)
	if len(groups) == 0 {
		reply.reply(emit, "", "draw.verifying")
		return
	}
	for _, key := range []replyKey{"draw.status.winners", "draw.status.paying", "draw.status.unpaid", "draw.status.no_show"} {
		if targets := engine.DrawTargets(groups[key]); targets != "" {
			reply.reply(emit, "", key, "targets", targets)
		}
	}
}

func drawWinnerStatus(kind draw.Kind, state draw.SlotState) replyKey {
	switch state {
	case draw.SlotWon:
		if kind == draw.KindRaffle {
			return "draw.status.paying"
		}
		return "draw.status.winners"
	case draw.SlotPaid, draw.SlotClaimed:
		return "draw.status.winners"
	case draw.SlotUnpaid:
		return "draw.status.unpaid"
	case draw.SlotNoShow:
		return "draw.status.no_show"
	default:
		return ""
	}
}
