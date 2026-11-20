// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

type refusedJoin struct {
	round   draw.Round
	viewer  draw.ViewerID
	verdict eligibility.Verdict
}

func drawJoiner(c *module.Context) (eligibility.Joiner, error) {
	viewer, err := bus.NonZeroUserID(c.Env.ChatterUserID)
	return eligibility.Joiner{Channel: c.BroadcasterID, Viewer: viewer, Role: c.Chatter()}, err
}

func drawViewerID(viewer uint64) draw.ViewerID { return draw.ViewerID(strconv.FormatUint(viewer, 10)) }

func roundRules(round draw.Round) eligibility.Rules {
	if round.Terms.Kind == draw.KindGiveaway {
		return eligibility.Parse(round.Terms.Rules)
	}
	return eligibility.Rules{}
}

func (d drawOwner) admit(ctx context.Context, rules eligibility.Rules, joiner eligibility.Joiner) eligibility.Verdict {
	verdict, err := d.deps.Eligibility.Join(ctx, rules, joiner)
	if err != nil {
		d.log.Warn("draw standings lookup failed", module.BIDField(joiner.Channel), zap.Error(err))
	}
	return verdict
}

func (d drawOwner) refuseJoin(ctx context.Context, c *module.Context, emit module.Emit, refusal refusedJoin) error {
	on, err := d.refusalLineOn(ctx, refusal.round)
	if err != nil {
		return err
	}
	first, _, err := d.deps.Draw.Refuse(ctx, draw.Refusal{
		Round: refusal.round, Viewer: refusal.viewer, Reason: string(refusal.verdict.Reason),
	})
	if first && on {
		newChatReplier(c).say(emit, eligibilityLine(c, refusal.verdict))
	}
	return err
}

func (d drawOwner) refusalLineOn(ctx context.Context, round draw.Round) (bool, error) {
	if round.Terms.Kind != draw.KindGiveaway {
		return false, nil
	}
	view, _, err := d.deps.Proj.Module(ctx, uint64(round.Channel), "giveaway")
	var config struct {
		Line    codec.RawMessage `json:"refusalLineOn"`
		Enabled codec.RawMessage `json:"refusalEnabled"`
	}
	_ = codec.Unmarshal(view.Configs, &config)
	raw := config.Line
	if len(raw) == 0 {
		raw = config.Enabled
	}
	switch string(raw) {
	case "false", `"false"`, `"off"`:
		return false, err
	case "true", `"true"`, `"on"`:
		return true, err
	default:
		return eligibility.Limits.RefusalLineOn, err
	}
}

func (d drawOwner) resolveCheck(ctx context.Context, c *module.Context) drawCommandState {
	round, found, err := d.activeJoin(ctx, c)
	if !found {
		return drawCommandState{err: err}
	}
	return drawCommandState{round: round, found: true}
}

func (d drawOwner) check(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
	state := drawState(ctx, c, d.resolveCheck)
	if state.err != nil {
		return state.err
	}
	joiner, err := drawJoiner(c)
	if err != nil {
		return nil
	}
	rules := roundRules(state.round)
	verdict := d.admit(ctx, rules, joiner)
	if !verdict.Pass {
		replyJoinCheck(c, emit, verdict, rules.DrawRules())
		return nil
	}
	result, err := d.deps.Eligibility.Verify(ctx, draw.Check{Round: state.round, Viewer: drawViewerID(joiner.Viewer)})
	if err != nil {
		d.log.Warn("draw eligibility lookup failed", c.BID(), zap.Error(err))
	}
	replyDrawCheck(c, emit, result, err)
	return nil
}

func replyDrawCheck(c *module.Context, emit module.Emit, result draw.Verdict, err error) {
	reply := newChatReplier(c)
	switch {
	case err != nil:
		reply.reply(emit, "", "eligibility.lookup_failed")
	case result.Pass:
		reply.reply(emit, "", "eligibility.check.ok")
	default:
		reply.reply(emit, "", replyKey("eligibility.refused."+result.Reason), "need", result.Detail["need"])
	}
}

func replyJoinCheck(c *module.Context, emit module.Emit, verdict eligibility.Verdict, later eligibility.Rules) {
	reply := newChatReplier(c)
	line := eligibilityLine(c, verdict)
	if requirements := engine.GiveawayRequirements(c.Locale, later); requirements != "" {
		line += " " + reply.render("", "eligibility.check.later", "requirements", requirements)
	}
	reply.say(emit, line)
}

func eligibilityLine(c *module.Context, result eligibility.Verdict) string {
	reply := newChatReplier(c)
	switch result.Reason {
	case eligibility.ReasonLookup:
		return reply.render("", "eligibility.lookup_failed")
	case eligibility.ReasonPoints, eligibility.ReasonWatch:
		return standingRequirementLine(c, result)
	default:
		return reply.render("", replyKey("eligibility.refused."+result.Reason))
	}
}

func standingRequirementLine(c *module.Context, result eligibility.Verdict) string {
	need := strconv.FormatInt(result.Need, 10)
	had := strconv.FormatInt(result.Had, 10)
	if result.Reason == eligibility.ReasonWatch {
		need = watchSeconds(result.Need).humanize(c)
		had = watchSeconds(result.Had).humanize(c)
	}
	return newChatReplier(c).render("", replyKey("eligibility.refused."+result.Reason),
		"need", need, "had", had, "time", result.AsOf.UTC().Format("15:04"))
}
