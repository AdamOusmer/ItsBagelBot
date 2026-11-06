// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/utils"

	"go.uber.org/zap"
)

type triggerMatch struct {
	plan  *automation.Plan
	args  string
	event map[string]string
	left  *automation.CooldownCondition
}

func (p *Pipeline) runAutomationStage(ctx context.Context, mctx *module.Context, views map[string]projection.ModuleView, consumed bool, emit module.Emit) {
	if consumed || p.automationsLocked(mctx) {
		return
	}
	env := &mctx.Env
	switch {
	case env.Type == subMessageKind:
		p.runSubMessageTriggers(ctx, mctx, p.channelIndex(ctx, mctx, views).SubMessage(), emit)
	case env.Type == chatType && len(env.Senders) == 0:
		p.runChatScanTriggers(ctx, mctx, p.channelIndex(ctx, mctx, views).ChatScan(), emit)
	}
}

func (p *Pipeline) runChatScanTriggers(ctx context.Context, mctx *module.Context, triggers []*automation.CompiledTrigger, emit module.Emit) {
	text := automation.NewMatchText(mctx.Env.Text)
	for _, t := range triggers {
		if args, ok := t.MatchChat(&text); ok {
			p.runChatTrigger(ctx, mctx, triggerMatch{plan: t.Plan, args: args, left: t.Left}, emit)
		}
	}
}

func (p *Pipeline) runSubMessageTriggers(ctx context.Context, mctx *module.Context, triggers []*automation.CompiledTrigger, emit module.Emit) {
	if len(triggers) == 0 {
		return
	}
	kind, ok := automation.LookupEventKind(subMessageKind)
	if !ok {
		return
	}
	event := kind.Extract(mctx.Env.Event)
	text := automation.NewMatchText(event["message"])
	for _, t := range triggers {
		if args, ok := t.MatchSubMessage(&text); ok {
			p.startAutomation(ctx, mctx, triggerMatch{plan: t.Plan, args: args, event: event}, emit)
		}
	}
}

func (p *Pipeline) automationsLocked(mctx *module.Context) bool {
	return p.automationsBeta && !mctx.Regress.IsPremium()
}

// Fails closed: a projection error must not run paid-only behavior.
func (p *Pipeline) automationsLockedFor(ctx context.Context, broadcasterID uint64) bool {
	if !p.automationsBeta {
		return false
	}
	u, err := p.proj.User(ctx, broadcasterID)
	return err != nil || !u.Premium()
}

func (p *Pipeline) runKindAutomations(ctx context.Context, mctx *module.Context, kind string, emit module.Emit) {
	if p.automationsLocked(mctx) {
		return
	}
	triggers := p.loadIndex(ctx, mctx).Kind(kind)
	if len(triggers) == 0 {
		return
	}
	event := triggers[0].Kind.Extract(mctx.Env.Event)
	for _, t := range triggers {
		if compiledPasses(t, event) {
			p.startAutomation(ctx, mctx, triggerMatch{plan: t.Plan, event: event}, emit)
		}
	}
}

func compiledPasses(t *automation.CompiledTrigger, event map[string]string) bool {
	if t.RewardID != "" {
		return event["reward_id"] == t.RewardID
	}
	return filtersPass(t.Kind, t.Filters, event)
}

func (p *Pipeline) channelIndex(ctx context.Context, mctx *module.Context, views map[string]projection.ModuleView) *automation.TriggerIndex {
	if !views[AutomationsModuleName].IsEnabled {
		return nil
	}
	return p.loadIndex(ctx, mctx)
}

func (p *Pipeline) loadIndex(ctx context.Context, mctx *module.Context) *automation.TriggerIndex {
	if p.automations.Source == nil || mctx.Env.Origin == "trial" {
		return nil
	}
	index, err := p.automations.Source.AutomationIndex(ctx, mctx.BroadcasterID)
	if err != nil {
		p.log.Warn("automations: load failed", module.BIDField(mctx.BroadcasterID), zap.Error(err))
		return nil
	}
	return index
}

func (p *Pipeline) startAutomation(ctx context.Context, mctx *module.Context, m triggerMatch, emit module.Emit) {
	plan := m.plan
	auto := &plan.Automation
	id, err := utils.NewID()
	if err != nil {
		p.log.Warn("automations: run id failed", module.BIDField(mctx.BroadcasterID), zap.Error(err))
		return
	}
	p.ensureLocale(ctx, mctx)
	run := getRun()
	defer putRun(run)
	fillTriggeredRun(run, mctx, m)
	run.ID = id.String()
	if EventIdentity(&mctx.Env) == "" {
		run.Identity = ":" + auto.ID
	}
	p.runAutomation(ctx, run, emit)
}

func fillTriggeredRun(run *AutomationRun, mctx *module.Context, m triggerMatch) {
	*run = AutomationRun{
		Plan:          m.plan,
		BroadcasterID: mctx.BroadcasterID,
		Channel:       mctx.Env.BroadcasterUserID,
		Regress:       mctx.Regress,
		Locale:        mctx.Locale,
		Env:           &mctx.Env,
		Ctx:           mctx,
		Viewer:        triggerViewer(&mctx.Env, m.event),
		Args:          m.args,
		Event:         m.event,
		Source:        m.plan.Automation.Definition.Trigger.Type,
	}
}

func triggerViewer(env *lane.Envelope, event map[string]string) Viewer {
	if env.Type == chatType {
		id, _ := strconv.ParseUint(env.ChatterUserID, 10, 64)
		return Viewer{ID: id, Login: env.ChatterUserLogin, Name: env.ChatterUserName}
	}
	id, _ := strconv.ParseUint(event["user_id"], 10, 64)
	return Viewer{ID: id, Login: event["login"], Name: event["user"]}
}

func filtersPass(kind automation.EventKind, filters map[string]string, event map[string]string) bool {
	for key, want := range filters {
		i := slices.IndexFunc(kind.Fields, func(f automation.EventField) bool { return f.Name == key })
		if i < 0 || !filterPasses(kind.Fields[i].Numeric, event[key], want) {
			return false
		}
	}
	return true
}

func filterPasses(numeric bool, got, want string) bool {
	if !numeric {
		return strings.EqualFold(got, want)
	}
	have, errGot := strconv.ParseInt(got, 10, 64)
	min, errWant := strconv.ParseInt(want, 10, 64)
	return errGot == nil && errWant == nil && have >= min
}
