// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync/atomic"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
)

const AutomationsModuleName = "automations"

type AutomationSource interface {
	Automations(ctx context.Context, broadcasterID uint64) ([]automation.Automation, error)
	AutomationIndex(ctx context.Context, broadcasterID uint64) (*automation.TriggerIndex, error)
}

type RunLog interface {
	Record(ctx context.Context, broadcasterID uint64, rec automationrpc.RunRecord)
	Recent(ctx context.Context, broadcasterID uint64, automationID string, limit int) ([]automationrpc.RunRecord, error)
}

type RunLimiter interface {
	AllowRun(ctx context.Context, broadcasterID uint64) (bool, error)
	FirstRefusal(broadcasterID uint64) bool
	AllowLights(ctx context.Context, broadcasterID uint64) (bool, error)
}

type AutomationDeps struct {
	Source    AutomationSource
	Runs      RunLog
	Limiter   RunLimiter
	Variables VariableStore
	Overlay   OverlayPublisher
	Queue     RunQueue
}

type AutomationRun struct {
	Plan          *automation.Plan
	BroadcasterID uint64
	Channel       string
	Regress       module.Regress
	Locale        string
	Env           *lane.Envelope
	Ctx           *module.Context
	Viewer        Viewer
	Args          string
	ArgMap        map[string]string
	Event         map[string]string
	Source        string
	ID            string
	Identity      string
	Depth         int
	Started       time.Time
	Admitted      bool
	Record        automationrpc.RunRecord
	Locals        map[string]string
	Iter          string
	Replay        string
	Tree          *runTree
	Slot          *queueSlot
	DetachedRole  module.Role
}

// A root run, its `run` children and every detached path spend these concurrently: a copy per path multiplies each cap.
type runTree struct {
	executed atomic.Int32
	visits   atomic.Int32
	chats    atomic.Int32
}

func (t *runTree) claimChats(want int) int {
	for {
		used := t.chats.Load()
		kept := min(int32(want), automation.MaxChatMessagesPerRun-used)
		if kept <= 0 {
			return 0
		}
		if t.chats.CompareAndSwap(used, used+kept) {
			return int(kept)
		}
	}
}

type noRunLog struct{}

func (noRunLog) Record(context.Context, uint64, automationrpc.RunRecord) {}

func (noRunLog) Recent(context.Context, uint64, string, int) ([]automationrpc.RunRecord, error) {
	return nil, nil
}

type openLimiter struct{}

func (openLimiter) AllowRun(context.Context, uint64) (bool, error) { return true, nil }

func (openLimiter) FirstRefusal(uint64) bool { return false }

func (openLimiter) AllowLights(context.Context, uint64) (bool, error) { return true, nil }

func withAutomationDefaults(d AutomationDeps) AutomationDeps {
	if d.Runs == nil {
		d.Runs = noRunLog{}
	}
	if d.Limiter == nil {
		d.Limiter = openLimiter{}
	}
	if d.Queue == nil {
		d.Queue = openRunQueue{}
	}
	return d
}

func (p *Pipeline) runAutomation(ctx context.Context, run *AutomationRun, emit module.Emit) {
	run.Started = time.Now()
	auto := &run.Plan.Automation
	run.Record = automationrpc.RunRecord{
		RunID: run.ID, AutomationID: auto.ID, Name: auto.Name,
		Trigger: auto.Definition.Trigger.Type, Source: run.Source, At: run.Started,
	}
	defer p.endRunSegment(ctx, run)
	admitting := run.Depth == 0 && !run.Admitted
	if admitting && !p.automationRunAllowed(ctx, run) {
		return
	}
	x := p.newActionExec(ctx, run, emit)
	defer releaseActionExec(x)
	pass, reason, err := x.conditionsPass()
	if err != nil || !pass {
		run.Record.Outcome, run.Record.Reason = skippedOutcome(err, reason)
		return
	}
	if p.admitToQueue(ctx, run) {
		x.execute()
	}
}

// Only the root owns a slot reference: a run child shares its parent's segment.
func (p *Pipeline) endRunSegment(ctx context.Context, run *AutomationRun) {
	if run.Depth == 0 {
		defer run.Slot.drop()
	}
	p.finishRun(ctx, run)
}

func (p *Pipeline) automationRunAllowed(ctx context.Context, run *AutomationRun) bool {
	ok, err := p.automations.Limiter.AllowRun(ctx, run.BroadcasterID)
	if err == nil && !ok {
		run.Record.Outcome, run.Record.Reason = automationrpc.OutcomeLimited, "channel_rate"
	}
	return err != nil || ok
}

func skippedOutcome(err error, reason string) (string, string) {
	if err != nil {
		return automationrpc.OutcomeFailed, err.Error()
	}
	return automationrpc.OutcomeSkipped, reason
}

// The run log reply must fit a NATS message, so long loops keep their first steps and how they ended.
func trimmedRecords(records []automationrpc.ActionRecord) []automationrpc.ActionRecord {
	if len(records) <= automation.MaxRecordedActions {
		return records
	}
	records[automation.MaxRecordedActions-1] = records[len(records)-1]
	return records[:automation.MaxRecordedActions]
}

func (p *Pipeline) finishRun(ctx context.Context, run *AutomationRun) {
	if run.Record.Outcome == outcomeDeferred {
		return
	}
	run.Record.DurationMS = int(time.Since(run.Started).Milliseconds())
	if run.Record.Outcome == "" {
		run.Record.Outcome = automationrpc.OutcomeOK
	}
	run.Record.Actions = trimmedRecords(run.Record.Actions)
	p.automations.Runs.Record(ctx, run.BroadcasterID, run.Record)
}
