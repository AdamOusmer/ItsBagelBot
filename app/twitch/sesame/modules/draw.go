// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
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
	round       draw.Round
	slots       []draw.Slot
	claims      draw.ClaimLookup
	found       bool
	legacy      raffleCmd
	legacyFound bool
	legacyOpen  bool
	aliasKey    string
	result      *engine.RaffleResult
	err         error
}

func (s drawCommandState) ownsCommand() bool {
	if s.round.ID != "" || s.found {
		return true
	}
	return s.legacyFound || s.err != nil
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

func (d drawOwner) resolveJoin(ctx context.Context, c *module.Context) drawCommandState {
	r, found, err := d.activeJoin(ctx, c)
	if err != nil || hasJoinRound(r.Phase, found) {
		return drawCommandState{round: r, found: found, err: err}
	}
	if d.deps.Raffle == nil {
		return drawCommandState{round: r}
	}
	legacy, err := d.deps.Raffle.Status(ctx, c.BroadcasterID)
	if legacy.Open {
		return drawCommandState{legacyFound: true, legacyOpen: true, err: err,
			aliasKey: "legacy:" + strconv.FormatUint(c.BroadcasterID, 10) + ":" + strconv.FormatInt(legacy.DeadlineMS, 10)}
	}
	return drawCommandState{round: r, legacyFound: wantsClosedRaffle(c), err: err}
}

func (d drawOwner) resolveEntryAlias(ctx context.Context, c *module.Context) drawCommandState {
	state := d.resolveJoin(ctx, c)
	switch {
	case state.found:
		state.aliasKey = string(state.round.ID)
	case !state.legacyOpen:
		return drawCommandState{err: state.err}
	}
	return state
}

func (d drawOwner) entryAlias(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
	state := drawState(ctx, c, d.resolveEntryAlias)
	if state.err != nil || state.aliasKey == "" {
		return state.err
	}
	key := "cd:draw:entry_alias:" + state.aliasKey + ":" + c.Env.ChatterUserID
	first, err := d.deps.Cooldown.Allow(ctx, key, draw.Pins.MaxOpen)
	if err != nil {
		return err
	}
	if !first {
		return d.join(ctx, c, args, emit)
	}
	return d.joinWithFreeNotice(ctx, c, emit)
}

func (d drawOwner) joinWithFreeNotice(ctx context.Context, c *module.Context, emit module.Emit) error {
	reply := newChatReplier(c)
	free := reply.render("", "draw.join.free")
	sent := false
	err := d.join(ctx, c, "", func(o *module.Output) {
		if !sent {
			o.Text += " " + free
			sent = true
		}
		emit(o)
	})
	if err == nil && !sent {
		reply.say(emit, free)
	}
	return err
}

func hasJoinRound(phase draw.Phase, found bool) bool {
	return found || phase == draw.PhaseOpen
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
		newChatReplier(c).reply(emit, "", "draw.join.closed")
		return nil
	case err != nil:
		return d.replyJoinLookupFailure(ctx, c, emit, draw.Check{Round: r, Viewer: viewer})
	case !verdict.Pass:
		return d.refuseJoin(ctx, c, refusedJoin{r, viewer, verdict})
	}
	return d.enterRound(ctx, c, emit, drawEntry{round: r, viewer: viewer, weight: verdict.Weight})
}

type drawEntry struct {
	round  draw.Round
	viewer draw.ViewerID
	weight int64
}

func (d drawOwner) enterRound(ctx context.Context, c *module.Context, emit module.Emit, entry drawEntry) error {
	joined, err := d.deps.Draw.Join(ctx, draw.JoinRequest{Channel: draw.ChannelID(c.BroadcasterID),
		Round: entry.round.ID, Viewer: entry.viewer, Login: strings.ToLower(c.Env.ChatterUserLogin), Weight: entry.weight})
	if err != nil {
		return err
	}
	switch {
	case entry.round.Terms.Kind == draw.KindRaffle:
		d.replyRaffleJoin(ctx, c, emit, joined)
	case !joined.Open:
		newChatReplier(c).reply(emit, "", "draw.join.closed")
	}
	return nil
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
	owns := found || anyDrawModuleOn(c)
	return drawCommandState{result: res, legacyFound: found, err: err, found: owns}
}

func anyDrawModuleOn(c *module.Context) bool {
	return c.ModuleOn(raffleModuleName) || c.ModuleOn(engine.GiveawayModuleName)
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
