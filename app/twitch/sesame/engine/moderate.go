// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

const (
	repEscalateThreshold  = 3
	repWarnToTimeoutScore = 1
	campaignThreshold     = 8
)

const (
	massRaidThreshold = 15
	massRaidBanCap    = 40
	raidCooldownTTL   = 60 * time.Second
)

func (p *Pipeline) moderateChat(ctx context.Context, mctx *module.Context, views map[string]projection.ModuleView, emit module.Emit) bool {
	if mctx.Env.Type != chatType {
		return false
	}
	amCfg := automodConfigFrom(views, p.automodLocked(mctx))
	if len(mctx.Env.Senders) > 0 {
		return p.gateCohort(ctx, mctx, amCfg, emit)
	}
	return p.gateChat(ctx, mctx, amCfg, emit)
}

func (p *Pipeline) automodLocked(mctx *module.Context) bool {
	return p.automodBeta && !mctx.Regress.IsPremium()
}

func (p *Pipeline) adaptiveEmoteCodes(mctx *module.Context) map[string]struct{} {
	if !p.adaptiveEnabled {
		return nil
	}
	return mctx.EmoteCodes()
}

func (p *Pipeline) gateChat(ctx context.Context, mctx *module.Context, amCfg *automod.Config, emit module.Emit) bool {
	if p.automod == nil {
		return false
	}
	env := &mctx.Env

	v, sigs := p.automod.Assess(mctx.Chatter(), env.Text, amCfg,
		automod.WithChannel(mctx.BroadcasterID),
		automod.WithChatter(env.ChatterUserID),
		automod.WithMessageEmotes(p.adaptiveEmoteCodes(mctx)))
	v, councilMint := p.campaignVote(ctx, voteInput{
		verdict:     v,
		simhash:     sigs.SimHash,
		linkish:     sigs.Linkish,
		broadcaster: mctx.BroadcasterID,
		sender:      env.ChatterUserID,
	})
	if v.Action == automod.ActionNone {
		return false
	}
	return p.applyChatVerdict(ctx, mctx, chatVerdict{v, councilMint}, emit)
}

type chatVerdict struct {
	verdict     automod.Verdict
	councilMint bool
}

func (p *Pipeline) applyChatVerdict(ctx context.Context, mctx *module.Context, decision chatVerdict, emit module.Emit) bool {
	env := &mctx.Env
	v, err := p.chatReputation(ctx, decision, ReputationEvent{Identity: EventIdentity(env), Viewer: env.ChatterUserID})
	if err != nil {
		p.log.Warn("reputation unavailable; enforcing base verdict", zap.Error(err))
		v = decision.verdict
	}
	actioned := false
	if p.automodEnforce {
		actioned = p.emitAutomod(v, env, emit)
	}
	id := mctx.BroadcasterID
	p.stats.record(ctx, func() { p.stats.flag(id, flagRule(v.Rule), actioned) })
	p.recordFlag(ctx, flagRule(v.Rule), actioned)
	monitor.TxnLogger(ctx, p.log).Info("automod verdict",
		zap.String("action", v.Action.String()),
		zap.String("rule", v.Rule),
		zap.Bool("enforced", actioned),
		module.BIDField(mctx.BroadcasterID),
		zap.String("chatter_id", env.ChatterUserID),
		shadowText(actioned, env.Text))
	return actioned
}

func (p *Pipeline) chatReputation(ctx context.Context, decision chatVerdict, event ReputationEvent) (automod.Verdict, error) {
	if !p.automodEnforce {
		return decision.verdict, nil
	}
	return p.reputationVerdict(ctx, decision, event)
}

func (p *Pipeline) reputationVerdict(ctx context.Context, decision chatVerdict, event ReputationEvent) (automod.Verdict, error) {
	if p.reputation == nil || decision.councilMint {
		return decision.verdict, nil
	}
	score, err := p.reputation.Observe(ctx, event)
	return escalateByReputation(decision.verdict, score), err
}

func shadowText(actioned bool, text string) zap.Field {
	if actioned {
		return zap.Skip()
	}
	return zap.String("text", text)
}

type voteInput struct {
	verdict     automod.Verdict
	simhash     uint64
	linkish     bool
	broadcaster uint64
	sender      string
}

func (p *Pipeline) campaignVote(ctx context.Context, in voteInput) (automod.Verdict, bool) {
	if p.campaign == nil || in.simhash == 0 {
		return in.verdict, false
	}
	if !in.linkish && in.verdict.Action != automod.ActionDelete {
		return in.verdict, false
	}
	if p.campaign.Observe(ctx, in.broadcaster, in.simhash, in.sender) < campaignThreshold {
		return in.verdict, false
	}

	p.log.Warn("campaign band quorum",
		module.BIDField(in.broadcaster),
		zap.String("chatter_id", in.sender))

	switch in.verdict.Action {
	case automod.ActionNone:
		return automod.Verdict{Action: automod.ActionDelete, Rule: "council:campaign"}, true
	case automod.ActionDelete:
		v := in.verdict
		v.Action = automod.ActionTimeout
		v.Seconds = 600
		v.Rule += "+campaign"
		return v, false
	}
	return in.verdict, false
}

func (p *Pipeline) gateCohort(ctx context.Context, mctx *module.Context, amCfg *automod.Config, emit module.Emit) bool {
	if p.automod == nil {
		return false
	}
	env := &mctx.Env
	msgEmotes := p.adaptiveEmoteCodes(mctx)

	broadcasterID, _ := env.BroadcasterID()
	sender := ""
	if len(env.Senders) > 0 {
		sender = env.Senders[0].ChatterUserID
	}
	v := p.automod.InspectWith(module.RoleEveryone, env.Text, amCfg,
		automod.WithChannel(broadcasterID),
		automod.WithChatter(sender),
		automod.WithMessageEmotes(msgEmotes))
	if v.Action == automod.ActionNone {
		return false
	}
	return p.applyCohortVerdict(ctx, cohortAction{v, broadcasterID, env}, emit)
}

func (p *Pipeline) applyCohortVerdict(ctx context.Context, action cohortAction, emit module.Emit) bool {
	v, broadcasterID, env := action.verdict, action.broadcasterID, action.env
	actioned := false
	if p.automodEnforce {
		p.bumpCohortReputation(ctx, env)
		actioned = p.emitCohort(ctx, cohortAction{v, broadcasterID, env}, emit)
	}
	p.stats.record(ctx, func() { p.stats.flag(broadcasterID, flagRule(v.Rule), actioned) })
	p.recordFlag(ctx, flagRule(v.Rule), actioned)
	monitor.TxnLogger(ctx, p.log).Info("automod cohort verdict",
		zap.String("action", v.Action.String()),
		zap.String("rule", v.Rule),
		zap.Int("cohort", len(env.Senders)),
		zap.Bool("enforced", actioned),
		module.BIDField(broadcasterID),
		shadowText(actioned, env.Text))
	return actioned
}

func (p *Pipeline) bumpCohortReputation(ctx context.Context, env *lane.Envelope) {
	if p.reputation == nil {
		return
	}
	for _, sender := range env.Senders {
		identity := sender.MsgID
		if identity == "" {
			identity = EventIdentity(env)
		}
		_, err := p.reputation.Observe(ctx, ReputationEvent{Identity: identity, Viewer: sender.ChatterUserID})
		if err != nil {
			p.log.Warn("cohort reputation unavailable; enforcing base verdict", zap.Error(err))
		}
	}
}

func escalateByReputation(v automod.Verdict, score int) automod.Verdict {
	if score >= repWarnToTimeoutScore && v.Action == automod.ActionWarn {
		v.Action = automod.ActionTimeout
		v.Seconds = 600
		v.Rule += "+repeat"
		return v
	}
	if score >= repEscalateThreshold && v.Action == automod.ActionTimeout {
		v.Action = automod.ActionBan
		v.Rule += "+repeat"
	}
	return v
}

type modTarget struct {
	broadcasterID string
	userID        string
	msgID         string
}

func (p *Pipeline) emitAutomod(v automod.Verdict, env *lane.Envelope, emit module.Emit) bool {
	target := modTarget{broadcasterID: env.BroadcasterUserID, userID: env.ChatterUserID, msgID: env.MsgID}
	acted := p.emitModeration(v, target, emit)
	if v.Action == automod.ActionWarn && env.MsgID != "" {
		del := automod.Verdict{Action: automod.ActionDelete, Rule: v.Rule}
		if p.emitModeration(del, target, emit) {
			acted = true
		}
	}
	return acted
}

func (p *Pipeline) emitModeration(v automod.Verdict, target modTarget, emit module.Emit) bool {
	o := GetOutput()
	switch v.Action {
	case automod.ActionBan:
		o.Type = outgress.TypeBan
	case automod.ActionTimeout:
		o.Type = outgress.TypeTimeout
		o.Duration = float64(v.Seconds)
	case automod.ActionWarn:
		o.Type = outgress.TypeWarn
	case automod.ActionDelete:
		if target.msgID == "" {
			PutOutput(o)
			return false
		}
		o.Type = outgress.TypeDelete
		o.MsgID = target.msgID
	default:
		PutOutput(o)
		return false
	}
	o.BroadcasterID = target.broadcasterID
	o.TargetUserID = target.userID
	o.Reason = "automod:" + v.Rule
	emit(o)
	PutOutput(o)
	return true
}

func isMassRaid(v automod.Verdict, distinctSenders int) bool {
	return distinctSenders >= massRaidThreshold &&
		(v.Action == automod.ActionTimeout || v.Action == automod.ActionBan)
}

func (p *Pipeline) shieldEscalates(v automod.Verdict, broadcasterID uint64, env *lane.Envelope) bool {
	if !p.shieldEnabled || !isMassRaid(v, len(env.Senders)) {
		return false
	}
	return p.raidGate.trip(broadcasterID, time.Now())
}

type cohortAction struct {
	verdict       automod.Verdict
	broadcasterID uint64
	env           *lane.Envelope
}

func (p *Pipeline) emitCohort(ctx context.Context, action cohortAction, emit module.Emit) bool {
	v, broadcasterID, env := action.verdict, action.broadcasterID, action.env
	acted := false
	if p.shieldEscalates(v, broadcasterID, env) {
		p.emitShield(env.BroadcasterUserID, emit)
		p.stats.record(ctx, func() { p.stats.flag(broadcasterID, ruleShieldMode, true) })
		p.recordFlag(ctx, ruleShieldMode, true)
		p.log.Warn("automod shield mode",
			module.BIDField(broadcasterID),
			zap.Int("cohort", len(env.Senders)),
			zap.String("rule", v.Rule))
		acted = true
	}

	limit := min(len(env.Senders), massRaidBanCap)
	for i := 0; i < limit; i++ {
		id := env.Senders[i].ChatterUserID
		if id == "" {
			continue
		}
		target := modTarget{broadcasterID: env.BroadcasterUserID, userID: id, msgID: env.Senders[i].MsgID}
		if p.emitModeration(v, target, emit) {
			acted = true
		}
	}
	return acted
}

func (p *Pipeline) emitShield(broadcasterID string, emit module.Emit) {
	o := GetOutput()
	o.Type = outgress.TypeShieldMode
	o.BroadcasterID = broadcasterID
	o.Reason = "automod:mass_raid"
	emit(o)
	PutOutput(o)
}
