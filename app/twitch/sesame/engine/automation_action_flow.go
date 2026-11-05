// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/internal/utils"

	"go.uber.org/zap"
)

const (
	continuationTimeout = 30 * time.Second
	maxSyncSegment      = 3 * time.Second
)

type autoExecState struct {
	running  atomic.Int32
	segments atomic.Int32
}

func (x *actionExec) stop(step, automation.EmptyParams) error {
	x.signal = signalStop
	return nil
}

func (x *actionExec) delay(_ step, p automation.DelayAction) error {
	if x.budget.delayMS+p.Ms > automation.MaxDelayMS {
		return limited("delay_budget")
	}
	x.budget.delayMS += p.Ms
	x.delayMS = p.Ms
	x.signal = signalDelay
	return nil
}

func (x *actionExec) randomPick(s step, p automation.RandomPickAction) error {
	k := x.pickBranch(s, p.Branches)
	if k < 0 {
		return failure("no_branch")
	}
	x.execList(s.Branches[k])
	return nil
}

// Each path pauses alone: its remaining steps move to their own continuation, so the next path starts right away.
// Each path also owns a copy of the locals: a path that pauses could not hand its writes back in order.
func (x *actionExec) parallel(s step, _ automation.ParallelAction) error {
	spent, locals := x.budget.delayMS, x.run.Locals
	for i := range s.Branches {
		x.budget.delayMS = spent
		x.run.Locals = maps.Clone(locals)
		x.execList(s.Branches[i])
		x.settlePath(s, i)
	}
	x.budget.delayMS, x.run.Locals = spent, locals
	return nil
}

func (x *actionExec) loop(s step, p automation.LoopAction) error {
	total, ok := automation.ParseLoopTimes(x.expand(p.Times))
	if !ok {
		return failure("bad_times")
	}
	prior, shadowed := x.run.Locals[localIndex]
	x.iterate(loopCursor{step: s, next: 1, total: total, prior: prior, shadowed: shadowed})
	return nil
}

type loopCursor struct {
	step     step
	next     int
	total    int
	prior    string
	shadowed bool
}

func (x *actionExec) iterate(c loopCursor) {
	outer := x.iter
	for c.next <= c.total {
		if x.run.Tree.executed.Load() >= automation.MaxActions {
			x.recordLimited(c.step, "max_actions")
			break
		}
		index := strconv.Itoa(c.next)
		c.next++
		x.iter = outer + "@" + index
		x.setLocal(localIndex, index)
		x.execList(c.step.Branches[0])
		x.iter = outer
		if x.signal != signalNone {
			x.pauseLoop(c, outer)
			return
		}
	}
	x.restoreIndex(c)
}

func (x *actionExec) pauseLoop(c loopCursor, outer string) {
	if x.signal == signalDelay {
		x.rest = append(x.rest, resume{loop: &c, iter: outer})
	}
}

func (x *actionExec) restoreIndex(c loopCursor) {
	if c.shadowed {
		x.setLocal(localIndex, c.prior)
		return
	}
	delete(x.run.Locals, localIndex)
}

func (x *actionExec) settlePath(s step, path int) {
	if x.signal == signalDelay && len(x.rest) > 0 {
		x.schedulePath(s, path)
	}
	x.signal, x.rest = signalNone, nil
}

func (x *actionExec) schedulePath(s step, path int) {
	cont := detachRun(x.run)
	// Node, path and iteration keep replay ids distinct: outgress drops a repeated one.
	cont.Replay = nestedReplayKey(x.run.replayKey(), s.EffectKey+"."+strconv.Itoa(path)+x.iter)
	cont.Durable = nil
	x.startContinuation(cont)
}

func (x *actionExec) pickBranch(s step, branches []automation.Branch) int {
	total := 0
	for _, b := range branches {
		total += max(b.Weight, 0)
	}
	if total <= 0 {
		return -1
	}
	n := x.branchRoll(s, total)
	for i, b := range branches {
		if n < b.Weight {
			return i
		}
		n -= b.Weight
	}
	return len(branches) - 1
}

// A taken-over segment repeats: a fresh roll could pick the other branch and run both branches' effects.
func (x *actionExec) branchRoll(s step, total int) int {
	if x.run.Durable == nil {
		return x.p.automationIntN(total)
	}
	sum := sha256.Sum256([]byte(x.run.replayKey() + "\x00" + s.EffectKey + x.iter))
	return int(binary.BigEndian.Uint64(sum[:8]) % uint64(total))
}

func (x *actionExec) ifElse(s step, _ automation.IfAction) error {
	pass, err := x.condition(s.Condition)
	if err != nil {
		return failure("condition_error")
	}
	if pass {
		x.execList(s.Branches[0])
		return nil
	}
	x.execList(s.Branches[1])
	return nil
}

func (x *actionExec) runOther(s step, p automation.RunAction) error {
	if x.run.Depth >= automation.MaxRecursionDepth {
		return limited("depth")
	}
	if x.p.automationsLockedFor(x.ctx, x.run.BroadcasterID) {
		return limited("premium_required")
	}
	target := x.findAutomation(p.AutomationID)
	if target == nil {
		return failure("not_found")
	}
	x.flush()
	child := x.childRun(s, target)
	x.p.runAutomation(x.ctx, child, x.emit)
	if child.Record.Outcome == automationrpc.OutcomeFailed {
		return failure("child_failed")
	}
	return succeeded(child.Record.Outcome)
}

func (x *actionExec) findAutomation(id string) *automation.Plan {
	src := x.p.automations.Source
	if src == nil {
		return nil
	}
	index, err := src.AutomationIndex(x.ctx, x.run.BroadcasterID)
	if err != nil {
		return nil
	}
	return index.Plan(id)
}

func (x *actionExec) childRun(s step, target *automation.Plan) *AutomationRun {
	child := *x.run
	child.Plan = target
	child.Depth++
	child.Record = automationrpc.RunRecord{}
	child.OwnsSlot, child.Durable = false, nil
	child.Locals = maps.Clone(x.run.Locals)
	child.Iter = x.iter + "~" + s.EffectKey
	child.Replay = nestedReplayKey(x.run.replayKey(), target.Automation.ID+child.Iter)
	child.ID = x.run.ID + "/" + target.Automation.ID
	if id, err := utils.NewID(); err == nil {
		child.ID = id.String()
	}
	return &child
}

func (x *actionExec) startContinuation(cont *AutomationRun) {
	budget := x.budget
	budget.segment++
	wait := time.Duration(x.delayMS) * time.Millisecond
	c := continuation{run: cont, budget: budget, rest: x.rest, wait: wait}
	if !x.p.durable.record(x.ctx, c) {
		return
	}
	cont.Slot.retain()
	x.p.autoExec.running.Add(1)
	go x.p.continueRun(context.WithoutCancel(x.ctx), c)
}

type continuation struct {
	run    *AutomationRun
	budget runBudget
	rest   []resume
	wait   time.Duration
}

func (p *Pipeline) continueRun(ctx context.Context, c continuation) {
	run, budget := c.run, c.budget
	defer p.autoExec.running.Add(-1)
	defer run.Slot.drop()
	ctx, cancel := context.WithTimeout(ctx, c.wait+continuationTimeout)
	defer cancel()
	if !p.awaitResume(ctx, run, c.wait) {
		return
	}
	p.autoExec.segments.Add(1)
	defer p.autoExec.segments.Add(-1)
	defer p.failPanickedSegment(ctx, run)
	if p.automationsLockedFor(ctx, run.BroadcasterID) {
		run.Record.Outcome, run.Record.Reason = automationrpc.OutcomeLimited, "premium_required"
		p.finishRun(ctx, run)
		return
	}
	out := p.detachedEmit(ctx, run.BroadcasterID, run.Locale, run.replayKey()+":"+strconv.Itoa(budget.segment))
	x := p.newActionExec(ctx, run, out.emit)
	defer releaseActionExec(x)
	x.budget = budget
	x.runLists(c.rest)
	x.conclude()
	if err := p.flushDetached(ctx, out); err != nil {
		p.log.Warn("automation continuation publish failed", zap.String("run_id", run.ID), zap.Error(err))
	}
	p.finishRun(ctx, run)
}

// A recorded run that crashed the process would be taken over and crash the next replica, and the next.
func (p *Pipeline) failPanickedSegment(ctx context.Context, run *AutomationRun) {
	if r := recover(); r != nil {
		p.log.Error("automation segment panicked", zap.String("run_id", run.ID), zap.Any("panic", r))
		run.Record.Outcome, run.Record.Reason = automationrpc.OutcomeFailed, "panic"
		p.finishRun(ctx, run)
	}
}

// Answers false when the run ended here or now belongs to another replica.
func (p *Pipeline) awaitResume(ctx context.Context, run *AutomationRun, wait time.Duration) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return p.durable.stillOurs(ctx, run)
	case <-p.durable.haltedFor(run):
		return false
	case <-ctx.Done():
		run.Record.Outcome, run.Record.Reason = automationrpc.OutcomeFailed, "canceled"
		p.finishRun(ctx, run)
		return false
	}
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func detachRun(run *AutomationRun) *AutomationRun {
	d := *run
	d.Ctx = nil
	if run.Ctx != nil {
		d.DetachedRole = run.Ctx.Chatter()
	}
	d.Env = detachEnvelope(run.Env)
	d.Channel, d.Locale, d.Args = strings.Clone(run.Channel), strings.Clone(run.Locale), strings.Clone(run.Args)
	d.ID, d.Identity, d.Replay = strings.Clone(run.ID), strings.Clone(run.Identity), strings.Clone(run.Replay)
	d.Viewer = Viewer{ID: run.Viewer.ID, Login: strings.Clone(run.Viewer.Login), Name: strings.Clone(run.Viewer.Name)}
	d.Iter = strings.Clone(run.Iter)
	d.ArgMap, d.Event, d.Locals = cloneStrings(run.ArgMap), cloneStrings(run.Event), cloneStrings(run.Locals)
	d.Record.Actions = slices.Clone(run.Record.Actions)
	return &d
}

func (run *AutomationRun) identity() string {
	if run.Env != nil {
		if id := EventIdentity(run.Env); id != "" {
			return id
		}
	}
	return run.Identity
}

// A run id is random per delivery, so replay keys start from the event; a run with no event identity has only its id.
func (run *AutomationRun) replayKey() string {
	if run.Replay != "" {
		return run.Replay
	}
	identity := run.identity()
	if identity == "" || strings.HasPrefix(identity, ":") {
		return run.ID
	}
	return identity + ">" + run.Plan.Automation.ID
}

func nestedReplayKey(parent, site string) string {
	sum := sha256.Sum256([]byte(parent + "\x00" + site))
	return hex.EncodeToString(sum[:16])
}

func cloneStrings(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[strings.Clone(k)] = strings.Clone(v)
	}
	return out
}

func detachEnvelope(e *lane.Envelope) *lane.Envelope {
	if e == nil {
		return nil
	}
	return &lane.Envelope{
		Type:                 strings.Clone(e.Type),
		EventID:              strings.Clone(e.EventID),
		Origin:               strings.Clone(e.Origin),
		BroadcasterUserID:    strings.Clone(e.BroadcasterUserID),
		BroadcasterUserLogin: strings.Clone(e.BroadcasterUserLogin),
		BroadcasterUserName:  strings.Clone(e.BroadcasterUserName),
		ChatterUserID:        strings.Clone(e.ChatterUserID),
		ChatterUserLogin:     strings.Clone(e.ChatterUserLogin),
		ChatterUserName:      strings.Clone(e.ChatterUserName),
		MsgID:                strings.Clone(e.MsgID),
		ChatMessageID:        strings.Clone(e.ChatMessageID),
		Event:                bytes.Clone(e.Event),
	}
}
