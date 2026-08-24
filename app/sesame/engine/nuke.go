// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/moderation"

	"go.uber.org/zap"
)

// The nuke budget. massRaidBanCap bounds one folded cohort's bans at 40; a
// sweep covers minutes of chat, so its budget is wider — but 100 timeout
// calls still ride inside Twitch's 800-action/min Helix budget alongside
// whatever else the lane is doing, and outgress's lane buckets pace them.
const (
	nukeMaxTargets     = 100
	nukeDefaultSeconds = 600
	// nukeMinSeconds floors the duration so a fat-fingered "!nuke spam 1"
	// cannot become a meaningless 1s ban wave; the ceiling is Twitch's own
	// maximum timeout (two weeks).
	nukeMinSeconds = 30
	nukeMaxSeconds = 14 * 24 * 60 * 60
	// The phrase floor keeps "!nuke a" from sweeping half the chat; it is
	// measured on the NORMALIZED phrase so leet ("h8") is judged as what it
	// says ("hate"), not as what was typed.
	nukeMinPhraseRunes = 3
	nukeMaxPhraseRunes = 120
)

// Nuke is the phrase-targeted mass-moderation service behind !nuke: it reads
// the sweep memory, times out every matched chatter within budget, and
// escalates to channel-level Shield Mode when the matches overrun the budget
// and Shield Mode is armed. nil in Deps leaves the command inert and records
// nothing.
type Nuke struct {
	// Recent is the sweep memory. Production wires ValkeyRecent (centralized:
	// the replica pool shares one durable consumer, so no pod sees a channel's
	// whole chat); RecentLog is the single-replica/test double.
	Recent recentStore
	// BotID is the bot's own parsed user id; the pipeline never records bot
	// chat anyway, this only guards against a stale record timing the bot out.
	BotID uint64

	log    *zap.Logger
	shield func(channelID) bool
	now    func() time.Time
}

// NewNuke builds the service over an empty recent log.
func NewNuke(recent recentStore, botID uint64, log *zap.Logger) *Nuke {
	if log == nil {
		log = zap.NewNop()
	}
	return &Nuke{Recent: recent, BotID: botID, log: log, now: time.Now}
}

// setShield wires the pipeline's escalation decision (armed + raid-gate dedup)
// after construction; modules hold the Nuke before the Pipeline exists.
func (n *Nuke) setShield(fn func(broadcasterID uint64) bool) {
	n.shield = func(id channelID) bool { return fn(uint64(id)) }
}

// setClock overrides the time source (tests).
func (n *Nuke) setClock(fn func() time.Time) { n.now = fn }

// recordChat retains one chat envelope into the sweep log. Called for every
// chat line that reaches the pipeline stages; the log itself filters command
// shapes and empty text.
func (n *Nuke) recordChat(broadcasterID uint64, env *lane.Envelope) {
	if n.Recent == nil || env.Type != chatType {
		return
	}
	n.Recent.Record(channelID(broadcasterID), env, n.now())
}

const nukeUsage = "usage: !nuke <phrase> [seconds] — sweeps the last 10m of chat for messages containing phrase"

// parseNukeArgs splits "!nuke" arguments into the phrase and the timeout
// seconds. A trailing token of bare digits (or digits+"s") is the duration;
// anything else leaves the default. A phrase-less invocation parses but
// yields the empty phrase, which Execute rejects as usage.
func parseNukeArgs(args string) (phrase string, seconds int64) {
	seconds = nukeDefaultSeconds
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return "", seconds
	}
	last := fields[len(fields)-1]
	if secs, ok := parseDurationToken(last); ok {
		seconds = secs
		fields = fields[:len(fields)-1]
	}
	return strings.Join(fields, " "), seconds
}

// parseDurationToken accepts "600" or "600s", clamped to [min,max]. Anything
// else is not a duration token (it stays part of the phrase).
func parseDurationToken(tok string) (int64, bool) {
	tok = strings.TrimSuffix(tok, "s")
	secs, err := strconv.ParseInt(tok, 10, 64)
	if err != nil {
		return 0, false
	}
	return min(max(secs, nukeMinSeconds), nukeMaxSeconds), true
}

// Execute runs one nuke: sweep the recent log for the phrase, drop trusted
// roles and protected ids, emit timeouts within budget, escalate to Shield
// Mode on overflow when armed, and answer with a summary line. It returns
// nil even on zero hits or bad usage — those are chat replies, not errors,
// and must never nack the invoking message.
func (n *Nuke) Execute(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
	phrase, secs := parseNukeArgs(args)
	norm := moderation.Normalize(GetBuf(), phrase)
	defer PutBuf(norm)
	runes := utf8.RuneCount(norm)
	if runes < nukeMinPhraseRunes || runes > nukeMaxPhraseRunes {
		emitChat(emit, c.Env.BroadcasterUserID, nukeUsage)
		return nil
	}

	hits := n.Recent.Sweep(ctx, channelID(c.BroadcasterID), phrase, n.now())
	matched := filterNukeTargets(hits, c.BroadcasterID, n.BotID)

	res := sweepResult{
		broadcaster: c.Env.BroadcasterUserID,
		tenant:      channelID(c.BroadcasterID),
		seconds:     secs,
		actioned:    min(len(matched), nukeMaxTargets),
	}
	res.overflow = max(len(matched)-nukeMaxTargets, 0)

	emitTimeouts(matched[:res.actioned], res, emit)
	shielded := n.escalateOnOverflow(res, emit)
	emitChat(emit, res.broadcaster, res.summary(shielded))

	n.log.Info("nuke executed",
		zap.Uint64("broadcaster_id", c.BroadcasterID),
		zap.Int("targets", res.actioned),
		zap.Int("matched", len(hits)),
		zap.Int("phrase_runes", runes),
		zap.Int64("seconds", secs),
		zap.Bool("shield", shielded),
		zap.String("by", c.Env.ChatterUserID),
	)
	return nil
}

func emitChat(emit module.Emit, broadcasterID, text string) {
	emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: broadcasterID, Text: text})
}

// filterNukeTargets drops everyone a phrase collision must never punish:
// VIPs and above (staff in the line of fire of their own raid cleanup),
// the broadcaster, and the bot.
func filterNukeTargets(hits []RecentHit, broadcasterID uint64, botID uint64) []RecentHit {
	protected := protectedIDs{broadcaster: channelID(broadcasterID), bot: channelID(botID)}
	targets := make([]RecentHit, 0, len(hits))
	for _, h := range hits {
		if !h.sweepable(protected) {
			continue
		}
		targets = append(targets, h)
	}
	return targets
}

// protectedIDs names the two identities a sweep must never punish no matter
// what they typed: the channel's owner and the bot itself.
type protectedIDs struct {
	broadcaster channelID
	bot         channelID
}

// sweepable reports whether a matched sender may take the punishment: staff
// (VIP and up), the broadcaster and the bot are never swept on a phrase hit.
func (h RecentHit) sweepable(p protectedIDs) bool {
	if h.Role >= module.RoleVIP {
		return false
	}
	return h.UserID != p.broadcaster && h.UserID != p.bot
}

// emitTimeouts translates the capped target list into Helix timeout jobs.
// The outgress lane buckets pace them within Twitch's action budget; this
// side only enforces its own per-sweep cap.
// emitTimeouts translates the capped target list into Helix timeout jobs.
// The outgress lane buckets pace them within Twitch's action budget; this
// side only enforces its own per-sweep cap.
func emitTimeouts(targets []RecentHit, res sweepResult, emit module.Emit) {
	id := GetBuf()
	defer PutBuf(id)
	for i := range targets {
		emit(&module.Output{
			Type:          outgress.TypeTimeout,
			BroadcasterID: res.broadcaster,
			TargetUserID:  string(strconv.AppendUint(id[:0], uint64(targets[i].UserID), 10)),
			Duration:      float64(res.seconds),
			Reason:        "nuke",
		})
	}
}

// escalateOnOverflow activates Shield Mode when matches overrun the budget,
// reporting whether it did. Only an armed policy escalates, and the
// pipeline's raid gate dedups the activation so a raid already escalated by
// the automod is not double-tripped.
func (n *Nuke) escalateOnOverflow(res sweepResult, emit module.Emit) bool {
	if res.overflow == 0 {
		return false // within budget: nothing to cover for
	}
	if n.shield == nil {
		return false // no armed policy: report the cap instead
	}
	if !n.shield(res.tenant) {
		return false // disarmed for this channel or raid-gated by the automod
	}
	o := GetOutput()
	o.Type = outgress.TypeShieldMode
	o.BroadcasterID = res.broadcaster
	o.Reason = "nuke:overflow"
	emit(o)
	PutOutput(o)
	return true
}

// sweepResult is what one !nuke invocation found and did. The reporting,
// capping and escalation helpers consume this one value instead of parallel
// primitives that drift apart when a new field joins the story. broadcaster
// is the raw channel id outgress outputs address; chan is the same tenant in
// its domain type for policy calls.
type sweepResult struct {
	broadcaster string
	tenant      channelID
	seconds     int64 // timeout length every target receives
	actioned    int   // senders actually timed out (within budget)
	overflow    int   // matched senders left over the budget cap
}

// summary renders the chat-facing outcome: what happened, and — when the
// budget capped the sweep — what covered the rest. The matched phrase stays
// out of the reply so a mod cannot make the bot echo spam back into chat.
func (res sweepResult) summary(shielded bool) string {
	if res.actioned == 0 {
		return "no recent messages matched — nothing nuked"
	}
	s := "🚯 nuked " + strconv.Itoa(res.actioned) + " user(s) with " + strconv.FormatInt(res.seconds, 10) + "s timeouts"
	switch {
	case shielded:
		s += " — over budget, Shield Mode activated"
	case res.overflow > 0:
		s += " — cap reached, " + strconv.Itoa(res.overflow) + " more left for Shield Mode"
	}
	return s
}

// shieldDecision is the pipeline's escalation policy, shared with the cohort
// path: only when Shield Mode is armed, and deduped per channel through the
// same raid gate, so a raid already escalated by the automod within the TTL
// window does not double-activate on nuke overflow.
func (p *Pipeline) shieldDecision(broadcasterID uint64) bool {
	if !p.shieldEnabled {
		return false
	}
	return p.raidGate.trip(broadcasterID, time.Now())
}
