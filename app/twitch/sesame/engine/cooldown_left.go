// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

const (
	cooldownLeftReplyKey  = "time.cooldown_left"
	cooldownReadyReplyKey = "time.cooldown_ready"
)

var errNoLongCooldownStore = errors.New("no long cooldown store")

// cooldownSubject names the windows one recipe keeps for one viewer.
type cooldownSubject struct {
	broadcasterID uint64
	viewerID      uint64
	automationID  string
}

func runCooldownSubject(run *AutomationRun) cooldownSubject {
	return cooldownSubject{broadcasterID: run.BroadcasterID, viewerID: run.Viewer.ID, automationID: run.Plan.Automation.ID}
}

func (s cooldownSubject) windows(c automation.CooldownCondition) []CooldownWindow {
	var windows []CooldownWindow
	bid := strconv.FormatUint(s.broadcasterID, 10)
	if c.GlobalS > 0 {
		windows = append(windows, CooldownWindow{Key: "cooldown:auto:" + bid + ":" + s.automationID, TTL: time.Duration(c.GlobalS) * time.Second})
	}
	if c.UserS > 0 {
		key := "cooldown:autouser:" + bid + ":" + strconv.FormatUint(s.viewerID, 10) + ":" + s.automationID
		windows = append(windows, CooldownWindow{Key: key, TTL: time.Duration(c.UserS) * time.Second})
	}
	return windows
}

// windowsLeft reports the time left of each running window in claim order, so the first entry is the window a claim would have been refused by. It never spends anything.
func (p *Pipeline) windowsLeft(ctx context.Context, broadcasterID uint64, windows []CooldownWindow) ([]time.Duration, error) {
	short, long := splitCooldownWindows(windows)
	left, err := p.longWindowsLeft(ctx, broadcasterID, long)
	if err != nil {
		return nil, err
	}
	for _, window := range short {
		remaining, err := p.cooldown.TimeLeft(ctx, window.Key)
		if err != nil {
			return nil, err
		}
		if remaining > 0 {
			left = append(left, remaining)
		}
	}
	return left, nil
}

func (p *Pipeline) longWindowsLeft(ctx context.Context, broadcasterID uint64, long []CooldownWindow) ([]time.Duration, error) {
	if len(long) == 0 {
		return nil, nil
	}
	store := p.automations.LongCooldowns
	if store == nil {
		return nil, errNoLongCooldownStore
	}
	live, err := store.PeekLongCooldowns(ctx, LongCooldownClaim{BroadcasterID: broadcasterID, Windows: long})
	if err != nil {
		return nil, err
	}
	nowMS := time.Now().UnixMilli()
	left := make([]time.Duration, 0, len(live))
	for _, window := range live {
		if window.ExpiresAtMS > nowMS {
			left = append(left, time.Duration(window.ExpiresAtMS-nowMS)*time.Millisecond)
		}
	}
	return left, nil
}

// A refused if already holds the outcome, so a failed lookup only leaves the token empty.
func (x *actionExec) noteCooldownLeft(cond *automation.PlannedCondition) {
	cooldown, ok := cond.Params.(automation.CooldownCondition)
	if !ok || !x.run.Plan.ShowsCooldownLeft {
		return
	}
	left, err := x.p.windowsLeft(x.ctx, x.run.BroadcasterID, runCooldownSubject(x.run).windows(cooldown))
	if err != nil || len(left) == 0 {
		return
	}
	x.run.CooldownEndMS = time.Now().Add(left[0]).UnixMilli()
}

func (s automationScope) cooldownLeft() string {
	if s.cooldownEndMS == 0 {
		return ""
	}
	return i18n.CompactDuration(s.locale, time.Duration(s.cooldownEndMS-time.Now().UnixMilli())*time.Millisecond)
}

// answerTimeLeft replies without starting a run, so it costs no budget, no queue seat and no window.
func (p *Pipeline) answerTimeLeft(ctx context.Context, c *module.Context, m triggerMatch, emit module.Emit) {
	p.ensureLocale(ctx, c)
	run := getRun()
	defer putRun(run)
	fillTriggeredRun(run, c, m)
	left, err := p.windowsLeft(ctx, c.BroadcasterID, runCooldownSubject(run).windows(*m.left))
	if err != nil {
		p.log.Warn("automations: time left lookup failed", module.BIDField(c.BroadcasterID), zap.Error(err))
		return
	}
	longest := time.Duration(0)
	if len(left) > 0 {
		longest = slices.Max(left)
		run.CooldownEndMS = time.Now().Add(longest).UnixMilli()
	}
	x := p.newActionExec(ctx, run, emit)
	defer releaseActionExec(x)
	o := x.out(outgress.TypeChat)
	o.Text = x.expand(timeLeftReply(*m.left, c.Locale, longest))
	if x.queueText(o) == nil {
		x.flush()
	}
}

func timeLeftReply(c automation.CooldownCondition, locale string, left time.Duration) string {
	if left > 0 {
		return cmp.Or(c.LeftReply, i18n.T(locale, cooldownLeftReplyKey))
	}
	return cmp.Or(c.ReadyReply, i18n.T(locale, cooldownReadyReplyKey))
}
