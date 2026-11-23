// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

const eligibilityLookupFailed replyKey = "eligibility.lookup_failed"

type refusedJoin struct {
	round   draw.Round
	viewer  draw.ViewerID
	verdict eligibility.Verdict
}

func drawJoiner(c *module.Context, round draw.Round) (eligibility.Joiner, error) {
	viewer, err := bus.NonZeroUserID(c.Env.ChatterUserID)
	subscriber := slices.ContainsFunc(c.Env.Badges, func(badge lane.Badge) bool {
		return badge.SetID == "subscriber" || badge.SetID == "founder"
	})
	return eligibility.Joiner{Channel: c.BroadcasterID, Viewer: viewer, Kind: round.Terms.Kind, Role: c.Chatter(), Subscriber: subscriber,
		Cutoff: eligibility.Cutoff{ID: round.Terms.SnapshotID, OpenedAt: round.OpenedAt}}, err
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
	if err != nil && !snapshotGone(err) {
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

func snapshotGone(err error) bool {
	var refusal bus.RPCReplyError
	return errors.As(err, &refusal) && refusal.Code == loyaltyrpc.RefusalSnapshotGone
}

func (d drawOwner) refuseJoin(ctx context.Context, c *module.Context, refusal refusedJoin) error {
	config, configErr := engine.ReadGiveawayConfig(ctx, d.deps.Proj, uint64(refusal.round.Channel))
	req := draw.Refusal{Round: refusal.round, Viewer: refusal.viewer, Reason: string(refusal.verdict.Reason)}
	if configErr == nil && config.RefusalEnabled {
		req.Notice = &draw.RefusedViewer{Login: strings.ToLower(c.Env.ChatterUserLogin), Reason: req.Reason, Need: refusal.verdict.Need}
	}
	_, _, err := d.deps.Draw.Refuse(ctx, req)
	return errors.Join(configErr, err)
}

func (d drawOwner) resolveCheck(ctx context.Context, c *module.Context) drawCommandState {
	round, found, err := d.activeJoin(ctx, c)
	if !found || !drawTriggerAllowed(c, round.Terms.Kind) {
		return drawCommandState{err: err}
	}
	return drawCommandState{round: round, found: true, err: err}
}

func (d drawOwner) check(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
	state := drawState(ctx, c, d.resolveCheck)
	if state.err != nil {
		return state.err
	}
	joiner, err := drawJoiner(c, state.round)
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
		d.replyJoinEligibility(ctx, c, emit, joinCheckResult{verdict: verdict, later: rules.DrawRules()})
	case rules.DrawRules() != (eligibility.Rules{}):
		d.verifyCheck(ctx, c, emit, draw.Check{Round: state.round, Viewer: drawViewerID(joiner.Viewer)})
	default:
		newChatReplier(c).reply(emit, "", "eligibility.check.ok")
	}
	return nil
}

func (d drawOwner) verifyCheck(ctx context.Context, c *module.Context, emit module.Emit, check draw.Check) {
	result, err := d.deps.Eligibility.Verify(ctx, check)
	reply := newChatReplier(c)
	line := reply.render("", "eligibility.check.ok")
	switch {
	case err != nil:
		d.log.Warn("draw eligibility lookup failed", c.BID(), zap.Error(err))
		line += " " + reply.render("", eligibilityLookupFailed)
	case result.Pass:
		line += " " + reply.render("", "eligibility.check.draw_ok")
	default:
		line += " " + reply.eligibilityRefusal(result)
	}
	reply.say(emit, line)
}

type joinCheckResult struct {
	verdict eligibility.Verdict
	later   eligibility.Rules
}

func (d drawOwner) replyJoinEligibility(ctx context.Context, c *module.Context, emit module.Emit, result joinCheckResult) {
	currency := engine.DrawCurrencyName(ctx, d.deps.Proj, c.BroadcasterID)
	reply := newGameReplier(c, currency)
	line := reply.eligibilityRefusal(joinVerdict(c, result.verdict))
	if requirements := engine.GiveawayRequirements(c.Locale, result.later); requirements != "" {
		line += " " + reply.render("", "eligibility.check.later", "requirements", requirements)
	}
	reply.say(emit, line)
}

func (r chatReplier) eligibilityRefusal(result draw.Verdict) string {
	return r.render("", replyKey("eligibility.refused."+result.Reason),
		"need", result.Detail["need"], "had", result.Detail["had"])
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
	return map[string]string{"need": need, "had": had}
}
