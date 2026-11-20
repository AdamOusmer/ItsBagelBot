// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"slices"
	"strconv"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

const eligibilityLookupFailed replyKey = "eligibility.lookup_failed"

type refusedJoin struct {
	round   draw.Round
	viewer  draw.ViewerID
	verdict eligibility.Verdict
}

func drawJoiner(c *module.Context, kind draw.Kind) (eligibility.Joiner, error) {
	viewer, err := bus.NonZeroUserID(c.Env.ChatterUserID)
	subscriber := slices.ContainsFunc(c.Env.Badges, func(badge lane.Badge) bool {
		return badge.SetID == "subscriber" || badge.SetID == "founder"
	})
	return eligibility.Joiner{Channel: c.BroadcasterID, Viewer: viewer, Kind: kind, Role: c.Chatter(), Subscriber: subscriber}, err
}

func drawViewerID(viewer uint64) draw.ViewerID { return draw.ViewerID(strconv.FormatUint(viewer, 10)) }

func roundRules(round draw.Round) (eligibility.Rules, error) {
	if round.Terms.Kind == draw.KindGiveaway {
		return eligibility.Parse(round.Terms.Rules)
	}
	return eligibility.Rules{}, nil
}

func (d drawOwner) admit(ctx context.Context, rules eligibility.Rules, joiner eligibility.Joiner) (eligibility.Verdict, error) {
	verdict, err := d.deps.Eligibility.Join(ctx, rules, joiner)
	if err != nil {
		d.log.Warn("draw standings lookup failed", module.BIDField(joiner.Channel), zap.Error(err))
	}
	return verdict, err
}

func (d drawOwner) replyJoinLookupFailure(ctx context.Context, c *module.Context, emit module.Emit, check draw.Check) error {
	key := "cd:draw:lookup_failed:" + string(check.Round.ID) + ":" + string(check.Viewer)
	first, err := d.deps.Cooldown.Allow(ctx, key, draw.Pins.MaxOpen)
	if first && err == nil {
		newChatReplier(c).reply(emit, "", eligibilityLookupFailed)
	}
	return err
}

func (d drawOwner) refuseJoin(ctx context.Context, c *module.Context, emit module.Emit, refusal refusedJoin) error {
	first, _, err := d.deps.Draw.Refuse(ctx, draw.Refusal{
		Round: refusal.round, Viewer: refusal.viewer, Reason: string(refusal.verdict.Reason),
	})
	if err != nil || !first {
		return err
	}
	config, err := engine.ReadGiveawayConfig(ctx, d.deps.Proj, uint64(refusal.round.Channel))
	if err != nil {
		return err
	}
	if config.RefusalEnabled {
		replyEligibility(c, emit, joinVerdict(c, refusal.verdict), eligibility.Rules{})
	}
	return nil
}

func (d drawOwner) resolveCheck(ctx context.Context, c *module.Context) drawCommandState {
	round, found, err := d.activeJoin(ctx, c)
	if !found {
		return drawCommandState{err: err}
	}
	return drawCommandState{round: round, found: true, err: err}
}

func (d drawOwner) check(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
	state := drawState(ctx, c, d.resolveCheck)
	if state.err != nil {
		return state.err
	}
	joiner, err := drawJoiner(c, state.round.Terms.Kind)
	if err != nil {
		return nil
	}
	rules, err := roundRules(state.round)
	if err != nil {
		return err
	}
	verdict, err := d.admit(ctx, rules, joiner)
	switch {
	case err != nil:
		newChatReplier(c).reply(emit, "", eligibilityLookupFailed)
	case !verdict.Pass:
		replyEligibility(c, emit, joinVerdict(c, verdict), rules.DrawRules())
	default:
		d.verifyCheck(ctx, c, emit, draw.Check{Round: state.round, Viewer: drawViewerID(joiner.Viewer)})
	}
	return nil
}

func (d drawOwner) verifyCheck(ctx context.Context, c *module.Context, emit module.Emit, check draw.Check) {
	result, err := d.deps.Eligibility.Verify(ctx, check)
	if err != nil {
		d.log.Warn("draw eligibility lookup failed", c.BID(), zap.Error(err))
		newChatReplier(c).reply(emit, "", eligibilityLookupFailed)
		return
	}
	replyEligibility(c, emit, result, eligibility.Rules{})
}

func replyEligibility(c *module.Context, emit module.Emit, result draw.Verdict, later eligibility.Rules) {
	reply := newChatReplier(c)
	if result.Pass {
		reply.reply(emit, "", "eligibility.check.ok")
		return
	}
	line := reply.render("", replyKey("eligibility.refused."+result.Reason),
		"need", result.Detail["need"], "had", result.Detail["had"], "time", result.Detail["time"])
	if requirements := engine.GiveawayRequirements(c.Locale, later); requirements != "" {
		line += " " + reply.render("", "eligibility.check.later", "requirements", requirements)
	}
	reply.say(emit, line)
}

func joinVerdict(c *module.Context, result eligibility.Verdict) draw.Verdict {
	verdict := draw.Verdict{Reason: string(result.Reason)}
	if result.Reason == eligibility.ReasonPoints || result.Reason == eligibility.ReasonWatch {
		verdict.Detail = standingDetails(c, result)
	}
	return verdict
}

func standingDetails(c *module.Context, result eligibility.Verdict) map[string]string {
	need := strconv.FormatInt(result.Need, 10)
	had := strconv.FormatInt(result.Had, 10)
	if result.Reason == eligibility.ReasonWatch {
		need = watchSeconds(result.Need).humanize(c)
		had = watchSeconds(result.Had).humanize(c)
	}
	return map[string]string{"need": need, "had": had, "time": result.AsOf.UTC().Format("15:04")}
}
