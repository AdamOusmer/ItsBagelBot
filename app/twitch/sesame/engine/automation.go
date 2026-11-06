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

type AutomationDeps struct {
	Source    AutomationSource
	Runs      RunLog
	Variables VariableStore
	Queue     RunQueue
	Durable   DurableStore

	LongCooldowns LongCooldownStore

	durableTiming durableTiming
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
	Record        automationrpc.RunRecord
	Locals        map[string]string
	CooldownEndMS int64
	Iter          string
	Replay        string
	Tree          *runTree
	Slot          *queueSlot
	OwnsSlot      bool
	DetachedRole  module.Role
	Durable       *durableTicket
}

// A root run, its `run` children and every detached path spend these concurrently: a copy per path multiplies each cap.
type runTree struct {
	executed atomic.Int32
	visits   atomic.Int32
}

type noRunLog struct{}

func (noRunLog) Record(context.Context, uint64, automationrpc.RunRecord) {}

func (noRunLog) Recent(context.Context, uint64, string, int) ([]automationrpc.RunRecord, error) {
	return nil, nil
}

func withAutomationDefaults(d AutomationDeps) AutomationDeps {
	if d.Runs == nil {
		d.Runs = noRunLog{}
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

// A child that joined its parent's slot shares the parent's reference.
func (p *Pipeline) endRunSegment(ctx context.Context, run *AutomationRun) {
	if run.OwnsSlot {
		defer run.Slot.drop()
	}
	p.finishRun(ctx, run)
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
	if run.Record.Outcome == outcomeDeferred || !p.durable.finish(ctx, run) {
		return
	}
	run.Record.DurationMS = int(time.Since(run.Started).Milliseconds())
	if run.Record.Outcome == "" {
		run.Record.Outcome = automationrpc.OutcomeOK
	}
	run.Record.Actions = trimmedRecords(run.Record.Actions)
	p.automations.Runs.Record(ctx, run.BroadcasterID, run.Record)
}
