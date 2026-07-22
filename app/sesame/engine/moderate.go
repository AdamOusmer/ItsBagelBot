package engine

import (
	"context"
	"time"

	"ItsBagelBot/app/sesame/automod"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"go.uber.org/zap"
)

// Reputation ladder thresholds: a first-strike warn becomes a timeout for a
// chatter with any recent strike, and a timeout becomes a ban for a repeat
// offender. campaignThreshold is the distinct-sender count at which the
// campaign juror's vote counts (see campaignVote).
const (
	repEscalateThreshold  = 3
	repWarnToTimeoutScore = 1
	campaignThreshold     = 8
)

// massRaid parameters. A folded cohort of this many distinct hostile senders is a
// mass raid: it escalates to one channel-level Shield Mode call rather than a ban
// per account, which at Twitch's 800-action/min Helix budget a large raid would
// blow. massRaidBanCap bounds how many per-account bans one fold may still emit
// (cleanup within budget); Shield Mode, when armed, gates the rest at the channel.
const (
	massRaidThreshold = 15
	massRaidBanCap    = 40
	raidCooldownTTL   = 60 * time.Second
)

// moderateChat runs the automod gate on a chat line before dispatch: the
// single-chatter gate (with the campaign and reputation jurors) or, for a
// folded duplicate cohort, the cohort gate with its mass-raid escalation.
// Non-chat events pass through untouched. Returns whether an enforcement
// action was emitted, which skips command dispatch and handlers for this line
// (the chatter is being actioned).
//
// The broadcaster's automod config comes from the "automod" ModuleView (nil =
// global default). The automod module (app/sesame/modules/automod.go)
// registers a chat handler, so the registry marks chat as needing ModuleViews
// and the row arrives here through the standard module path.
func (p *Pipeline) moderateChat(ctx context.Context, mctx *module.Context, views map[string]projection.ModuleView, emit module.Emit) bool {
	if mctx.Env.Type != chatType {
		return false
	}
	amCfg := automodConfigFrom(views)
	if len(mctx.Env.Senders) > 0 {
		return p.gateCohort(ctx, &mctx.Env, amCfg, emit)
	}
	return p.gateChat(ctx, mctx, amCfg, emit)
}

// gateChat inspects one chatter's line. With enforcement on, a ban/timeout
// verdict is emitted and command dispatch + handlers are skipped for this line;
// a verdict we cannot yet enforce is logged. With enforcement off it is
// shadow-logged only.
func (p *Pipeline) gateChat(ctx context.Context, mctx *module.Context, amCfg *automod.Config, emit module.Emit) bool {
	if p.automod == nil {
		return false
	}
	env := &mctx.Env

	v, sigs := p.automod.Assess(mctx.Chatter(), env.Text, amCfg)
	v = p.campaignVote(ctx, v, sigs, env)
	if v.Action == automod.ActionNone {
		return false
	}

	// Reputation juror: repeat offenders climb the ladder (warn -> timeout ->
	// ban), then this hit is recorded against the chatter.
	if p.reputation != nil {
		v = escalateByReputation(v, p.reputation.Score(ctx, env.ChatterUserID))
		p.reputation.Bump(ctx, env.ChatterUserID)
	}

	actioned := false
	if p.automodEnforce {
		actioned = p.emitAutomod(v, env, emit)
	}
	p.log.Info("automod verdict",
		zap.String("action", v.Action.String()),
		zap.String("rule", v.Rule),
		zap.Bool("enforced", actioned),
		zap.Uint64("broadcaster_id", mctx.BroadcasterID),
		zap.String("chatter_id", env.ChatterUserID),
		shadowText(actioned, env.Text))
	return actioned
}

// shadowText carries the flagged line's content on a shadow (unenforced)
// verdict so rule quality can be judged from the log; an enforced verdict
// skips it to keep actioned moderation logs content-free.
func shadowText(actioned bool, text string) zap.Field {
	if actioned {
		return zap.Skip()
	}
	return zap.String("text", text)
}

// campaignVote consults the campaign juror (the council's cross-sender vote):
// the valkey distinct-sender count for this line's template, when the line is
// either already content-flagged at delete level or an unflagged link carrier.
// Corroboration escalates a delete to a timeout; on its own it only adds the
// mildest action (delete), never a punishment - abstain in favor of the user.
// Observe is HLL-idempotent per sender.
func (p *Pipeline) campaignVote(ctx context.Context, v automod.Verdict, sigs automod.Signals, env *lane.Envelope) automod.Verdict {
	if p.campaign == nil || sigs.SimHash == 0 {
		return v
	}
	if !sigs.Linkish && v.Action != automod.ActionDelete {
		return v
	}
	if p.campaign.Observe(ctx, sigs.SimHash, env.ChatterUserID) < campaignThreshold {
		return v
	}

	switch v.Action {
	case automod.ActionNone:
		return automod.Verdict{Action: automod.ActionDelete, Rule: "council:campaign"}
	case automod.ActionDelete:
		v.Action = automod.ActionTimeout
		v.Seconds = 600
		v.Rule += "+campaign"
	}
	return v
}

// gateCohort handles a folded duplicate cohort: plain chat the ingress squash
// collapsed identical lines from many chatters into env.Senders. Fan reputation
// out over every sender so a coordinated duplicate flood builds each
// participant's score, then inspect the shared text once: a hostile cohort (a
// slur/scam/IP-logger line posted in unison) is a raid. A large one escalates
// to channel-level Shield Mode instead of banning account by account; a small
// one is banned directly. A clean cohort (hype copypasta) trips nothing and
// only builds reputation.
func (p *Pipeline) gateCohort(ctx context.Context, env *lane.Envelope, amCfg *automod.Config, emit module.Emit) bool {
	if p.reputation != nil {
		for i := range env.Senders {
			p.reputation.Bump(ctx, env.Senders[i].ChatterUserID)
		}
	}
	if p.automod == nil {
		return false
	}

	// Cohort senders are untrusted viewers: the squash folds only plain
	// duplicate chat, so a trusted VIP/mod is judged on content here too.
	v := p.automod.InspectWith(module.RoleEveryone, env.Text, amCfg)
	if v.Action == automod.ActionNone {
		return false
	}

	broadcasterID, _ := env.BroadcasterID()
	actioned := false
	if p.automodEnforce {
		actioned = p.emitCohort(v, broadcasterID, env, emit)
	}
	p.log.Info("automod cohort verdict",
		zap.String("action", v.Action.String()),
		zap.String("rule", v.Rule),
		zap.Int("cohort", len(env.Senders)),
		zap.Bool("enforced", actioned),
		zap.Uint64("broadcaster_id", broadcasterID),
		shadowText(actioned, env.Text))
	return actioned
}

// escalateByReputation climbs the ladder against a repeat offender: warn ->
// timeout (any recent strike) -> ban (repeat threshold). Delete and stronger
// verdicts than the rule allows are unchanged.
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

// modTarget identifies who a moderation verdict lands on: the channel, the
// chatter, and (for deletes) the offending message.
type modTarget struct {
	broadcasterID string
	userID        string
	msgID         string
}

// emitAutomod translates an enforced single-chatter verdict into outgress
// moderation actions and emits them, returning whether anything was emitted.
// A warn verdict also deletes the offending message (the warn is the formal
// strike; the message should not stay up while the chatter acknowledges it).
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

// emitModeration maps a verdict to one outgress moderation Output against one
// target and emits it, returning whether an action was actually emitted. Ban,
// timeout, warn and delete are wired to Helix; a delete without a message id
// cannot be executed and is skipped (the caller's log line still records the
// verdict). Restrict has no public Helix write API and is never emitted.
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

// isMassRaid reports whether a folded cohort with a punishing content verdict is
// large enough to warrant channel-level Shield Mode. A delete/warn verdict (e.g. a
// caps heuristic on hype copypasta) is never a raid, so only timeout and ban
// qualify.
func isMassRaid(v automod.Verdict, distinctSenders int) bool {
	return distinctSenders >= massRaidThreshold &&
		(v.Action == automod.ActionTimeout || v.Action == automod.ActionBan)
}

// shieldEscalates reports whether a cohort verdict triggers the channel-level
// Shield Mode escalation: the feature is armed, the cohort qualifies as a mass
// raid, and this channel's raid gate has not already tripped for a recent fold.
func (p *Pipeline) shieldEscalates(v automod.Verdict, broadcasterID uint64, env *lane.Envelope) bool {
	if !p.shieldEnabled || !isMassRaid(v, len(env.Senders)) {
		return false
	}
	return p.raidGate.trip(broadcasterID, time.Now())
}

// emitCohort enforces a hostile folded cohort. A mass raid (large + punishing)
// escalates to one Shield Mode activation (deduped per channel) when Shield Mode
// is armed, then a bounded prefix of the cohort is banned as cleanup; a smaller
// cohort is banned outright. Returns whether any action was emitted.
func (p *Pipeline) emitCohort(v automod.Verdict, broadcasterID uint64, env *lane.Envelope, emit module.Emit) bool {
	acted := false
	if p.shieldEscalates(v, broadcasterID, env) {
		p.emitShield(env.BroadcasterUserID, emit)
		p.log.Warn("automod shield mode",
			zap.Uint64("broadcaster_id", broadcasterID),
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

// emitShield activates a channel's Shield Mode, the mass-raid channel-level
// defense. broadcasterID is the raw string channel id; outgress adds the
// moderator id and the {"is_active":true} body.
func (p *Pipeline) emitShield(broadcasterID string, emit module.Emit) {
	o := GetOutput()
	o.Type = outgress.TypeShieldMode
	o.BroadcasterID = broadcasterID
	o.Reason = "automod:mass_raid"
	emit(o)
	PutOutput(o)
}
