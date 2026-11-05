// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/outgress"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
)

const outcomeDeferred = "deferred"

var ErrActionUnsupported = errors.New("action unsupported")

type statusError struct{ status, detail string }

func (e statusError) Error() string { return e.detail }

func (e statusError) Is(target error) bool {
	return target == ErrActionUnsupported && e.status == automationrpc.StatusUnsupported
}

func failure(detail string) error { return statusError{automationrpc.StatusFailed, detail} }

func limited(detail string) error { return statusError{automationrpc.StatusLimited, detail} }

func skipped(detail string) error { return statusError{automationrpc.StatusSkipped, detail} }

func unsupported(detail string) error { return statusError{automationrpc.StatusUnsupported, detail} }

func succeeded(detail string) error { return statusError{automationrpc.StatusOK, detail} }

func classify(err error) (status, detail string) {
	if err == nil {
		return automationrpc.StatusOK, ""
	}
	if se, ok := err.(statusError); ok {
		return se.status, se.detail
	}
	return classifyWrapped(err)
}

func classifyWrapped(err error) (status, detail string) {
	var se statusError
	if errors.As(err, &se) {
		return se.status, se.detail
	}
	return automationrpc.StatusFailed, err.Error()
}

type signal int

const (
	signalNone signal = iota
	signalStop
	signalDelay
)

// iter is the loop iteration path the frame resumes under; it scopes effect dedup.
type resume struct {
	steps []automation.Step
	loop  *loopCursor
	iter  string
}

type step = *automation.Step

type runBudget struct {
	executed int
	chats    int
	delayMS  int
	segment  int
	visits   int
	handled  int
}

type actionExec struct {
	p       *Pipeline
	ctx     context.Context
	run     *AutomationRun
	emit    module.Emit
	bid     string
	budget  runBudget
	flushes int
	pending []module.Output
	signal  signal
	delayMS int
	rest    []resume
	tctx    *module.Context
	vars    runVars
	buf     []byte
	iter    string
	started time.Time
}

func (p *Pipeline) newActionExec(ctx context.Context, run *AutomationRun, emit module.Emit) *actionExec {
	x := getActionExec()
	x.p, x.ctx, x.run, x.emit = p, ctx, run, emit
	x.iter, x.started = run.Iter, time.Now()
	x.bid = run.Channel
	if x.bid == "" {
		x.bid = strconv.FormatUint(run.BroadcasterID, 10)
	}
	return x
}

func (x *actionExec) execute() {
	run := x.run
	if run.Plan.StepCount > 0 {
		run.Record.Actions = make([]automationrpc.ActionRecord, 0, run.Plan.StepCount)
	}
	x.budget.executed, x.budget.visits = run.Spent.executed, run.Spent.visits
	x.execList(run.Plan.Steps)
	run.Spent = stepSpend{executed: x.budget.executed, visits: x.budget.visits}
	x.conclude()
}

func (x *actionExec) runLists(lists []resume) {
	for k, r := range lists {
		x.resumeFrame(r)
		switch x.signal {
		case signalDelay:
			x.rest = append(x.rest, lists[k+1:]...)
			return
		case signalStop:
			return
		}
	}
}

func (x *actionExec) resumeFrame(r resume) {
	x.iter = r.iter
	if r.loop != nil {
		x.iterate(*r.loop)
		return
	}
	x.execList(r.steps)
}

func (x *actionExec) execList(list []automation.Step) {
	for i := range list {
		x.step(&list[i])
		x.yieldIfSlow()
		if x.signal == signalNone {
			continue
		}
		if x.signal == signalDelay && i+1 < len(list) {
			x.rest = append(x.rest, resume{steps: list[i+1:], iter: x.iter})
		}
		return
	}
}

// Fetches and store calls block the event lane; past the budget the remaining steps move to a continuation.
func (x *actionExec) yieldIfSlow() {
	budget := x.p.autoSegment
	if budget == 0 {
		budget = maxSyncSegment
	}
	if x.signal == signalNone && time.Since(x.started) > budget {
		x.signal, x.delayMS = signalDelay, 1
	}
}

func (x *actionExec) step(s step) {
	if x.budget.visits >= automation.MaxStepVisitsPerRun {
		x.recordLimited(s, "max_steps")
		x.signal = signalStop
		return
	}
	x.budget.visits++
	x.run.Record.Actions = append(x.run.Record.Actions, automationrpc.ActionRecord{Index: s.Index, Type: s.Type, Status: automationrpc.StatusOK})
	slot := len(x.run.Record.Actions) - 1
	status, detail := classify(x.dispatch(s))
	x.run.Record.Actions[slot].Status, x.run.Record.Actions[slot].Detail = status, detail
}

func (x *actionExec) dispatch(s step) error {
	if !s.Enabled {
		return skipped("disabled")
	}
	handler, known := actionTable[s.Type]
	if !known {
		return failure("unknown_type")
	}
	if automation.IsBranching(s.Type) {
		return handler(x, s)
	}
	if x.budget.executed >= automation.MaxActions {
		return limited("max_actions")
	}
	x.budget.executed++
	return handler(x, s)
}

func (x *actionExec) outcome() string {
	good, bad := tallyRecords(x.run.Record.Actions)
	switch {
	case bad <= x.budget.handled:
		return automationrpc.OutcomeOK
	case good == 0:
		return automationrpc.OutcomeFailed
	default:
		return automationrpc.OutcomePartial
	}
}

func (x *actionExec) conclude() {
	x.flush()
	if x.signal != signalDelay || len(x.rest) == 0 {
		x.run.Record.Outcome = x.outcome()
		return
	}
	if x.scheduleRest() {
		x.run.Record.Outcome = outcomeDeferred
		return
	}
	x.limitRest()
	x.run.Record.Outcome = x.outcome()
}

func (x *actionExec) limitRest() {
	for _, r := range x.rest {
		if r.loop != nil {
			x.recordLimited(r.loop.step, "delay_capacity")
		}
		for i := range r.steps {
			x.recordLimited(&r.steps[i], "delay_capacity")
		}
	}
}

func (x *actionExec) recordLimited(s step, detail string) {
	x.run.Record.Actions = append(x.run.Record.Actions, automationrpc.ActionRecord{
		Index: s.Index, Type: s.Type, Status: automationrpc.StatusLimited, Detail: detail,
	})
}

func (x *actionExec) out(typ string) module.Output {
	return module.Output{Type: typ, BroadcasterID: x.bid}
}

func (x *actionExec) queue(o module.Output) error {
	if !x.p.prepareCommand(&o) {
		return skipped("empty")
	}
	x.pending = append(x.pending, o)
	return nil
}

func (x *actionExec) queueText(o module.Output) error {
	start := len(x.pending)
	if o.Type == outgress.TypeChat {
		x.pending = x.p.appendChatLines(x.pending, &o)
	} else if x.p.prepareCommand(&o) {
		x.pending = append(x.pending, o)
	}
	added := len(x.pending) - start
	if added == 0 {
		return skipped("empty")
	}
	room := automation.MaxChatMessagesPerRun - x.budget.chats
	if room <= 0 {
		x.pending = x.pending[:start]
		return limited("chat_cap")
	}
	kept := min(added, room)
	x.pending = x.pending[:start+kept]
	x.budget.chats += kept
	return nil
}

func (x *actionExec) flush() {
	// emit must not retain items: the buffer is reused after the call.
	items := x.pending
	switch len(items) {
	case 0:
	case 1:
		x.emit(&items[0])
	default:
		x.emitBatch(items)
	}
	clear(items)
	x.pending = items[:0]
}

func (x *actionExec) emitBatch(items []module.Output) {
	x.flushes++
	batch := GetOutput()
	batch.Type = outgress.TypeBatch
	batch.BroadcasterID = x.bid
	batch.BatchID = x.run.ID + ":" + strconv.Itoa(x.budget.segment) + ":" + strconv.Itoa(x.flushes)
	batch.Items = items
	x.emit(batch)
	PutOutput(batch)
}

func (x *actionExec) identity() string {
	if x.run.Env != nil {
		if id := EventIdentity(x.run.Env); id != "" {
			return id
		}
	}
	return x.run.Identity
}
