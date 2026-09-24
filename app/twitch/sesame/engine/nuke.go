// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/moderation"

	"go.uber.org/zap"
)

const (
	nukeMaxTargets     = 100
	nukeDefaultSeconds = 600
	nukeMinSeconds     = 30
	nukeMaxSeconds     = 14 * 24 * 60 * 60
	nukeMinPhraseRunes = 3
	nukeMaxPhraseRunes = 120
)

type Nuke struct {
	Recent recentStore
	BotID  uint64

	log    *zap.Logger
	shield func(channelID) bool
	now    func() time.Time
}

func NewNuke(recent recentStore, botID uint64, log *zap.Logger) *Nuke {
	if log == nil {
		log = zap.NewNop()
	}
	return &Nuke{Recent: recent, BotID: botID, log: log, now: time.Now}
}

func (n *Nuke) setShield(fn func(broadcasterID uint64) bool) {
	n.shield = func(id channelID) bool { return fn(uint64(id)) }
}

func (n *Nuke) setClock(fn func() time.Time) { n.now = fn }

func (n *Nuke) recordChat(broadcasterID uint64, env *lane.Envelope) {
	if n.Recent == nil || env.Type != chatType {
		return
	}
	n.Recent.Record(channelID(broadcasterID), env, n.now())
}

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

func parseDurationToken(tok string) (int64, bool) {
	tok = strings.TrimSuffix(tok, "s")
	secs, err := strconv.ParseInt(tok, 10, 64)
	if err != nil {
		return 0, false
	}
	return min(max(secs, nukeMinSeconds), nukeMaxSeconds), true
}

func (n *Nuke) Execute(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
	phrase, secs := parseNukeArgs(args)
	norm := moderation.Normalize(GetBuf(), phrase)
	defer PutBuf(norm)
	runes := utf8.RuneCount(norm)
	if runes < nukeMinPhraseRunes || runes > nukeMaxPhraseRunes {
		emitChat(emit, c.Env.BroadcasterUserID, i18n.T(c.Locale, "nuke.usage"))
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
	emitChat(emit, res.broadcaster, res.summary(c.Locale, shielded))

	n.log.Info("nuke executed",
		module.BIDField(c.BroadcasterID),
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

type protectedIDs struct {
	broadcaster channelID
	bot         channelID
}

func (h RecentHit) sweepable(p protectedIDs) bool {
	if h.Role >= module.RoleVIP {
		return false
	}
	return h.UserID != p.broadcaster && h.UserID != p.bot
}

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

func (n *Nuke) escalateOnOverflow(res sweepResult, emit module.Emit) bool {
	if res.overflow == 0 {
		return false
	}
	if n.shield == nil {
		return false
	}
	if !n.shield(res.tenant) {
		return false
	}
	o := GetOutput()
	o.Type = outgress.TypeShieldMode
	o.BroadcasterID = res.broadcaster
	o.Reason = "nuke:overflow"
	emit(o)
	PutOutput(o)
	return true
}

type sweepResult struct {
	broadcaster string
	tenant      channelID
	seconds     int64
	actioned    int
	overflow    int
}

func (res sweepResult) summary(locale string, shielded bool) string {
	if res.actioned == 0 {
		return i18n.T(locale, "nuke.none")
	}
	s := i18n.T(locale, "nuke.actioned")
	s = strings.ReplaceAll(s, "{count}", strconv.Itoa(res.actioned))
	s = strings.ReplaceAll(s, "{seconds}", strconv.FormatInt(res.seconds, 10))
	switch {
	case shielded:
		s += " " + i18n.T(locale, "nuke.shielded")
	case res.overflow > 0:
		cap := i18n.T(locale, "nuke.capped")
		cap = strings.ReplaceAll(cap, "{count}", strconv.Itoa(res.overflow))
		s += " " + cap
	}
	return s
}

func (p *Pipeline) shieldDecision(broadcasterID uint64) bool {
	if !p.shieldEnabled {
		return false
	}
	return p.raidGate.trip(broadcasterID, time.Now())
}
