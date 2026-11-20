// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
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
	m.Command("join").Everyone().Yields().When(ownsDrawCommand(owner.resolveJoin)).Run(owner.join)
	m.Command("check").Everyone().Yields().When(ownsDrawCommand(owner.resolveCheck)).UserCooldown(eligibility.Limits.CheckCooldown).Run(owner.check)
	m.Command("claim").Everyone().Yields().When(ownsDrawCommand(owner.resolveClaim)).Run(owner.claim)
	m.Command("winner").Everyone().Yields().When(ownsDrawCommand(owner.resolveWinner)).Cooldown(draw.Pins.WinnerCooldown).Run(owner.winner)
	return m.Build()
}

type drawOwner struct {
	deps engine.Deps
	log  *zap.Logger
}

type drawCommandState struct {
	round       draw.Round
	slots       []draw.Slot
	claims      draw.ClaimLookup
	found       bool
	legacy      raffleCmd
	legacyFound bool
	result      *engine.RaffleResult
	err         error
}

type drawResolve func(context.Context, *module.Context) drawCommandState

func ownsDrawCommand(resolve drawResolve) func(context.Context, *module.Context) bool {
	return func(ctx context.Context, c *module.Context) bool {
		state := resolve(ctx, c)
		c.CommandState = state
		return state.round.ID != "" || state.found || state.legacyFound || state.err != nil
	}
}

func drawState(ctx context.Context, c *module.Context, resolve drawResolve) drawCommandState {
	if state, ok := c.CommandState.(drawCommandState); ok {
		return state
	}
	return resolve(ctx, c)
}

func (d drawOwner) activeJoin(ctx context.Context, c *module.Context) (draw.Round, bool, error) {
	if d.deps.Draw == nil {
		return draw.Round{}, false, nil
	}
	return d.deps.Draw.Joinable(ctx, draw.ChannelID(c.BroadcasterID))
}

func (d drawOwner) resolveJoin(ctx context.Context, c *module.Context) drawCommandState {
	r, found, err := d.activeJoin(ctx, c)
	if r.ID != "" || err != nil {
		return drawCommandState{round: r, found: found, err: err}
	}
	if d.deps.Raffle == nil {
		return drawCommandState{}
	}
	legacy, err := d.deps.Raffle.Status(ctx, c.BroadcasterID)
	return drawCommandState{legacyFound: legacy.Open || wantsClosedRaffle(c), err: err}
}

func wantsClosedRaffle(c *module.Context) bool {
	return c.ModuleOn(raffleModuleName) && !c.ModuleOn(queueModuleName)
}

func (d drawOwner) join(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
	state := drawState(ctx, c, d.resolveJoin)
	if state.err != nil {
		return state.err
	}
	if state.found {
		return d.joinRound(ctx, c, state.round, emit)
	}
	if state.round.ID != "" {
		newChatReplier(c).reply(emit, "", "draw.join.closed")
		return nil
	}
	if rc, ok := d.legacy(ctx, c); ok {
		return rc.join(ctx, "", emit)
	}
	return nil
}

func (d drawOwner) joinRound(ctx context.Context, c *module.Context, r draw.Round, emit module.Emit) error {
	joiner, err := drawJoiner(c)
	if err != nil {
		return nil
	}
	viewer := drawViewerID(joiner.Viewer)
	verdict := d.admit(ctx, roundRules(r), joiner)
	if !verdict.Pass {
		return d.refuseJoin(ctx, c, emit, refusedJoin{r, viewer, verdict})
	}
	joined, err := d.deps.Draw.Join(ctx, draw.JoinRequest{Channel: draw.ChannelID(c.BroadcasterID),
		Round: r.ID, Viewer: viewer, Login: strings.ToLower(c.Env.ChatterUserLogin), Weight: verdict.Weight})
	if err == nil && !joined.Open {
		newChatReplier(c).reply(emit, "", "draw.join.closed")
	}
	return err
}

func (d drawOwner) resolveClaim(ctx context.Context, c *module.Context) drawCommandState {
	claims, err := d.lookupClaims(ctx, c)
	if claims.Found || err != nil {
		return drawCommandState{claims: claims, found: claims.Found, err: err}
	}
	rc, found, err := d.legacyClaim(ctx, c)
	return drawCommandState{legacy: rc, legacyFound: found, err: err}
}

func (d drawOwner) lookupClaims(ctx context.Context, c *module.Context) (draw.ClaimLookup, error) {
	if d.deps.Draw == nil {
		return draw.ClaimLookup{}, nil
	}
	return d.deps.Draw.LookupClaims(ctx, draw.ChannelID(c.BroadcasterID), draw.ViewerID(c.Env.ChatterUserID))
}

func (d drawOwner) claim(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
	state := drawState(ctx, c, d.resolveClaim)
	if state.err != nil {
		return state.err
	}
	if state.legacyFound {
		return state.legacy.claimConfirm(ctx, emit)
	}
	if !state.found {
		return nil
	}
	outcome, err := d.deps.Draw.ClaimResolved(ctx, state.claims)
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
		return d.claimLegacyFallback(ctx, c, emit)
	}
	return nil
}

func (d drawOwner) claimLegacyFallback(ctx context.Context, c *module.Context, emit module.Emit) error {
	rc, found, err := d.legacyClaim(ctx, c)
	if err != nil || !found {
		return err
	}
	return rc.claimConfirm(ctx, emit)
}

func (d drawOwner) settling(ctx context.Context, c *module.Context) (draw.Round, []draw.Slot, bool, error) {
	if d.deps.Draw == nil {
		return draw.Round{}, nil, false, nil
	}
	return d.deps.Draw.Settling(ctx, draw.ChannelID(c.BroadcasterID), "")
}

func (d drawOwner) resolveWinner(ctx context.Context, c *module.Context) drawCommandState {
	r, slots, found, err := d.settling(ctx, c)
	if found || err != nil {
		return drawCommandState{round: r, slots: slots, found: found, err: err}
	}
	res, found, err := d.legacyResult(ctx, c)
	owns := found || c.ModuleOn(raffleModuleName) || c.ModuleOn("giveaway")
	return drawCommandState{result: res, legacyFound: found, err: err, found: owns}
}

func (d drawOwner) winner(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
	state := drawState(ctx, c, d.resolveWinner)
	if state.err != nil {
		return state.err
	}
	if state.round.ID != "" {
		replyDrawWinner(c, emit, state.round, state.slots)
		return nil
	}
	return d.legacyWinner(ctx, c, emit, state)
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
