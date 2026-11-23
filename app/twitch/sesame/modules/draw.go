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
	ownsJoin := ownsDrawCommand(owner.resolveJoin)
	m.Command("join").Everyone().Yields().When(func(ctx context.Context, c *module.Context) bool {
		return ownsJoin(ctx, c) || wantsClosedRaffle(c)
	}).Run(owner.join)
	m.Command("enter").Aliases("ticket").Everyone().Yields().When(ownsDrawCommand(owner.resolveEntryAlias)).Run(owner.entryAlias)
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
	round  draw.Round
	slots  []draw.Slot
	claims draw.ClaimLookup
	found  bool
	err    error
}

func (s drawCommandState) ownsCommand() bool {
	if s.round.ID != "" || s.found {
		return true
	}
	return s.err != nil
}

type drawResolve func(context.Context, *module.Context) drawCommandState

func ownsDrawCommand(resolve drawResolve) func(context.Context, *module.Context) bool {
	return func(ctx context.Context, c *module.Context) bool {
		state := resolve(ctx, c)
		c.CommandState = state
		return state.ownsCommand()
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

func drawTriggerAllowed(c *module.Context, kind draw.Kind) bool {
	switch kind {
	case draw.KindRaffle:
		return !c.TriggerDisabled(raffleModuleName, module.TriggerName(c.Command))
	case draw.KindGiveaway:
		return !c.TriggerDisabled(engine.GiveawayModuleName, module.TriggerName(c.Command))
	default:
		return true
	}
}

func hasActiveRound(s drawCommandState) bool {
	return s.found || s.round.ID != ""
}

func giveawayWinnerAllowed(c *module.Context) bool {
	return c.ModuleOn(engine.GiveawayModuleName) && !c.TriggerDisabled(engine.GiveawayModuleName, module.TriggerName(c.Command))
}

func (d drawOwner) resolveJoin(ctx context.Context, c *module.Context) drawCommandState {
	r, found, err := d.activeJoin(ctx, c)
	state := drawCommandState{round: r, found: found, err: err}
	if hasActiveRound(state) && !drawTriggerAllowed(c, r.Terms.Kind) {
		return drawCommandState{err: err}
	}
	return state
}

func (d drawOwner) resolveEntryAlias(ctx context.Context, c *module.Context) drawCommandState {
	state := d.resolveJoin(ctx, c)
	if state.found {
		return state
	}
	return drawCommandState{err: state.err}
}

func (d drawOwner) entryAlias(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
	state := drawState(ctx, c, d.resolveEntryAlias)
	return d.joinResolved(ctx, c, state, joinOutput{emit: emit, alias: true})
}

type joinOutput struct {
	emit  module.Emit
	alias bool
}

func (o joinOutput) reply(c *module.Context, accepted bool, send func(module.Emit)) {
	if !o.alias || !accepted {
		send(o.emit)
		return
	}
	reply := newChatReplier(c)
	free := reply.render("", "draw.join.free")
	sent := false
	send(func(out *module.Output) {
		if !sent {
			out.Text += " " + free
			sent = true
		}
		o.emit(out)
	})
	if !sent {
		reply.say(o.emit, free)
	}
}

func wantsClosedRaffle(c *module.Context) bool {
	if c.TriggerDisabled(raffleModuleName, module.TriggerName(c.Command)) {
		return false
	}
	return c.ModuleOn(raffleModuleName) && !c.ModuleOn(queueModuleName)
}

func (d drawOwner) join(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
	state := drawState(ctx, c, d.resolveJoin)
	return d.joinResolved(ctx, c, state, joinOutput{emit: emit})
}

func (d drawOwner) joinResolved(ctx context.Context, c *module.Context, state drawCommandState, output joinOutput) error {
	if state.err != nil {
		return state.err
	}
	if state.found {
		return d.joinRound(ctx, c, state.round, output)
	}
	if state.round.ID != "" {
		newChatReplier(c).reply(output.emit, "", "draw.join.closed")
		return nil
	}
	if wantsClosedRaffle(c) {
		d.replyRaffleJoin(ctx, c, output.emit, draw.JoinOutcome{})
	}
	return nil
}

func (d drawOwner) joinRound(ctx context.Context, c *module.Context, r draw.Round, output joinOutput) error {
	joiner, err := drawJoiner(c, r)
	if err != nil {
		return nil
	}
	rules, err := roundRules(r)
	if err != nil {
		return err
	}
	viewer := drawViewerID(joiner.Viewer)
	verdict, err := d.admit(ctx, rules, joiner)
	switch {
	case snapshotGone(err):
		newChatReplier(c).reply(output.emit, "", "draw.join.closed")
		return nil
	case err != nil:
		return d.replyJoinLookupFailure(ctx, c, output.emit, draw.Check{Round: r, Viewer: viewer})
	case !verdict.Pass:
		return d.refuseJoin(ctx, c, refusedJoin{r, viewer, verdict})
	}
	return d.enterRound(ctx, c, output, drawEntry{round: r, viewer: viewer, weight: verdict.Weight})
}

type drawEntry struct {
	round  draw.Round
	viewer draw.ViewerID
	weight int64
}

func (d drawOwner) enterRound(ctx context.Context, c *module.Context, output joinOutput, entry drawEntry) error {
	joined, err := d.deps.Draw.Join(ctx, draw.JoinRequest{Channel: draw.ChannelID(c.BroadcasterID),
		Round: entry.round.ID, Viewer: entry.viewer, Login: strings.ToLower(c.Env.ChatterUserLogin), Weight: entry.weight})
	if err != nil {
		return err
	}
	output.reply(c, joined.Added, func(emit module.Emit) {
		switch {
		case entry.round.Terms.Kind == draw.KindRaffle:
			d.replyRaffleJoin(ctx, c, emit, joined)
		case !joined.Open:
			newChatReplier(c).reply(emit, "", "draw.join.closed")
		}
	})
	return nil
}

func (d drawOwner) resolveClaim(ctx context.Context, c *module.Context) drawCommandState {
	if c.TriggerDisabled(engine.GiveawayModuleName, module.TriggerName(c.Command)) {
		return drawCommandState{}
	}
	claims, err := d.lookupClaims(ctx, c)
	return drawCommandState{claims: claims, found: claims.Found, err: err}
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
	}
	return nil
}

func (d drawOwner) settling(ctx context.Context, c *module.Context) (draw.Round, []draw.Slot, bool, error) {
	if d.deps.Draw == nil {
		return draw.Round{}, nil, false, nil
	}
	return d.deps.Draw.Settling(ctx, draw.ChannelID(c.BroadcasterID), "")
}

func (d drawOwner) resolveWinner(ctx context.Context, c *module.Context) drawCommandState {
	r, slots, found, err := d.settling(ctx, c)
	if r.ID == "" {
		return drawCommandState{found: giveawayWinnerAllowed(c), err: err}
	}
	if !drawTriggerAllowed(c, r.Terms.Kind) {
		return drawCommandState{err: err}
	}
	return drawCommandState{round: r, slots: slots, found: found, err: err}
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
	if !giveawayWinnerAllowed(c) {
		return nil
	}
	replied, err := d.lastGiveaway(ctx, c, emit)
	if err == nil && !replied {
		newChatReplier(c).reply(emit, "", "draw.status.none")
	}
	return err
}

func (d drawOwner) lastGiveaway(ctx context.Context, c *module.Context, emit module.Emit) (bool, error) {
	if d.deps.GiveawayWinners == nil {
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
