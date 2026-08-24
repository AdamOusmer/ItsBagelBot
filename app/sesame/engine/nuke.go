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
	shield func(broadcasterID uint64) bool
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
func (n *Nuke) setShield(fn func(broadcasterID uint64) bool) { n.shield = fn }

// setClock overrides the time source (tests).
func (n *Nuke) setClock(fn func() time.Time) { n.now = fn }

// recordChat retains one chat envelope into the sweep log. Called for every
// chat line that reaches the pipeline stages; the log itself filters command
// shapes and empty text.
func (n *Nuke) recordChat(broadcasterID uint64, env *lane.Envelope) {
	if n.Recent == nil || env.Type != chatType {
		return
	}
	n.Recent.Record(broadcasterID, env, n.now())
}

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
		emitChat(emit, c.Env.BroadcasterUserID, "usage: !nuke <phrase> [seconds] — sweeps the last 10m of chat for messages containing phrase")
		return nil
	}

	hits := n.Recent.Sweep(ctx, c.BroadcasterID, phrase, n.now())
	targets := make([]RecentHit, 0, len(hits))
	for _, h := range hits {
		if h.Role >= module.RoleVIP || h.UserID == c.BroadcasterID || h.UserID == n.BotID {
			continue // never sweep staff, the broadcaster, or the bot
		}
		targets = append(targets, h)
	}
	overflow := max(len(targets)-nukeMaxTargets, 0)
	targets = targets[:min(len(targets), nukeMaxTargets)]

	broadcasterID := c.Env.BroadcasterUserID
	id := GetBuf()
	defer PutBuf(id)
	for i := range targets {
		emit(&module.Output{
			Type:          outgress.TypeTimeout,
			BroadcasterID: broadcasterID,
			TargetUserID:  string(strconv.AppendUint(id[:0], targets[i].UserID, 10)),
			Duration:      float64(secs),
			Reason:        "nuke",
		})
	}

	shielded := overflow > 0 && n.shield != nil && n.shield(c.BroadcasterID)
	if shielded {
		o := GetOutput()
		o.Type = outgress.TypeShieldMode
		o.BroadcasterID = broadcasterID
		o.Reason = "nuke:overflow"
		emit(o)
		PutOutput(o)
	}

	switch {
	case len(targets) == 0:
		emitChat(emit, broadcasterID, "no recent messages matched — nothing nuked")
	case shielded:
		emitChat(emit, broadcasterID, "🚯 nuked "+strconv.Itoa(len(targets))+
			" user(s) with "+strconv.FormatInt(secs, 10)+"s timeouts — over budget, Shield Mode activated")
	case overflow > 0:
		emitChat(emit, broadcasterID, "🚯 nuked "+strconv.Itoa(len(targets))+
			" user(s) with "+strconv.FormatInt(secs, 10)+"s timeouts — cap reached, "+strconv.Itoa(overflow)+" more left for Shield Mode")
	default:
		emitChat(emit, broadcasterID, "🚯 nuked "+strconv.Itoa(len(targets))+
			" user(s) with "+strconv.FormatInt(secs, 10)+"s timeouts")
	}

	n.log.Info("nuke executed",
		zap.Uint64("broadcaster_id", c.BroadcasterID),
		zap.Int("targets", len(targets)),
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
