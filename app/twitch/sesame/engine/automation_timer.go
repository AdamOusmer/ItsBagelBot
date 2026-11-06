// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/utils"

	"go.uber.org/zap"
)

const autoTimerPrefix = "auto:"

func (p *Pipeline) automationsEnabled(ctx context.Context, broadcasterID uint64) bool {
	_, state, err := ModuleLookup{Proj: p.proj, BroadcasterID: broadcasterID, Name: AutomationsModuleName, Absent: ModuleOff}.Resolve(ctx)
	if err != nil {
		p.log.Warn("automations: module state read failed", module.BIDField(broadcasterID), zap.Error(err))
	}
	return state == ModuleOn
}

func (p *Pipeline) timerAutomations(ctx context.Context, broadcasterID uint64) []automation.Automation {
	if p.automations.Source == nil || p.automationsLockedFor(ctx, broadcasterID) {
		return nil
	}
	if !p.automationsEnabled(ctx, broadcasterID) {
		return nil
	}
	list, err := p.automations.Source.Automations(ctx, broadcasterID)
	if err != nil {
		p.log.Warn("automations: timer load failed", module.BIDField(broadcasterID), zap.Error(err))
		return nil
	}
	var timers []automation.Automation
	for _, a := range list {
		if a.Enabled && a.Definition.Trigger.Type == "timer" {
			timers = append(timers, a)
		}
	}
	return timers
}

func (s *ValkeyTimerStore) automationTimers(ctx context.Context, broadcasterID uint64) []timerDef {
	if s.pipeline == nil {
		return nil
	}
	var defs []timerDef
	for _, a := range s.pipeline.timerAutomations(ctx, broadcasterID) {
		params, err := automation.DecodeParams[automation.TimerTrigger](a.Definition.Trigger.Params)
		if err != nil {
			continue
		}
		defs = append(defs, automationTimerDef(a.ID, params))
	}
	return defs
}

func automationTimerDef(automationID string, p automation.TimerTrigger) timerDef {
	return timerDef{
		ID:           autoTimerPrefix + automationID,
		Interval:     p.IntervalS,
		Enabled:      true,
		MinChatLines: p.MinChatLines,
		AllowOffline: p.OfflineOK,
		JitterS:      p.RandomJitterS,
	}
}

func armJitter(td timerDef) time.Duration {
	if td.JitterS <= 0 {
		return 0
	}
	return time.Duration(rand.IntN(td.JitterS+1)) * time.Second
}

func (s *ValkeyTimerStore) fireAutomation(ctx context.Context, at armedTimer) {
	if s.pipeline == nil {
		return
	}
	autoID := strings.TrimPrefix(at.ref.id, autoTimerPrefix)
	s.pipeline.runTimerAutomation(ctx, at.ref.broadcasterID, autoID, s.now())
}

func (p *Pipeline) runTimerAutomation(ctx context.Context, broadcasterID uint64, automationID string, firedAt time.Time) {
	plan, ok := p.timerPlan(ctx, broadcasterID, automationID)
	if !ok {
		return
	}
	id, err := utils.NewID()
	if err != nil {
		p.log.Warn("automations: timer run id failed", zap.Error(err))
		return
	}
	run := &AutomationRun{
		Plan:          plan,
		BroadcasterID: broadcasterID,
		Channel:       strconv.FormatUint(broadcasterID, 10),
		Viewer:        Viewer{ID: broadcasterID},
		Source:        "timer",
		ID:            id.String(),
		Identity:      "timer:" + automationID + ":" + strconv.FormatInt(firedAt.Unix(), 10),
	}
	out := p.detachedEmit(ctx, broadcasterID, "", run.Identity)
	p.runAutomation(ctx, run, out.emit)
	if err := p.flushDetached(ctx, out); err != nil {
		p.log.Warn("automations: timer output failed", module.BIDField(broadcasterID), zap.String("automation_id", automationID), zap.Error(err))
	}
}

func (p *Pipeline) timerPlan(ctx context.Context, broadcasterID uint64, automationID string) (*automation.Plan, bool) {
	for _, a := range p.timerAutomations(ctx, broadcasterID) {
		if a.ID != automationID {
			continue
		}
		index, err := p.automations.Source.AutomationIndex(ctx, broadcasterID)
		if err != nil {
			p.log.Warn("automations: timer load failed", module.BIDField(broadcasterID), zap.Error(err))
			return nil, false
		}
		plan := index.Plan(automationID)
		return plan, plan != nil
	}
	return nil, false
}
