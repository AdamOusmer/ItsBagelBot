// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"sync/atomic"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/utils"
	"go.uber.org/zap"
)

type countdownAdmission struct {
	saved  atomic.Bool
	failed atomic.Bool
}

func (run *AutomationRun) failCountdownAdmission() bool {
	if run.CountdownAdmission == nil {
		return true
	}
	run.CountdownAdmission.failed.Store(true)
	return false
}

type CountdownResult struct {
	Found, Capped bool
	ExpiresAtMS   int64
}
type CountdownDue struct {
	BroadcasterID           uint64
	Name, Generation, Token string
	ExpiresAtMS             int64
	Progress                map[string]string
}
type CountdownRef struct {
	BroadcasterID uint64
	Name          string
}
type CountdownMutation struct {
	Ref         CountdownRef
	Seconds     int64
	OperationID string
}
type CountdownCheckpoint struct{ Key, Status string }
type CountdownStore interface {
	StartCountdown(context.Context, CountdownMutation) (CountdownResult, error)
	AddCountdown(context.Context, CountdownMutation) (CountdownResult, error)
	PeekCountdown(context.Context, CountdownRef) (CountdownResult, error)
	ClaimCountdown(context.Context) (CountdownDue, bool, error)
	AckCountdown(context.Context, CountdownDue) error
	ProgressCountdown(context.Context, CountdownDue, CountdownCheckpoint) error
}

func (x *actionExec) countdownStart(s step, p automation.CountdownStartAction) error {
	name, ok := automation.VariableName(p.Name)
	if !ok {
		return failure("bad_name")
	}
	if x.p.automations.Countdowns == nil {
		return failure("no_countdowns")
	}
	result, err := x.p.automations.Countdowns.StartCountdown(x.ctx, x.countdownMutation(s, name, int64(p.Seconds)))
	if err != nil {
		if errors.Is(err, ErrVariableLimit) {
			return limited("countdown_limit")
		}
		return failure("store_error")
	}
	x.run.CountdownEndMS = result.ExpiresAtMS
	return nil
}
func (x *actionExec) countdownAdd(s step, p automation.CountdownAddAction) error {
	name, ok := automation.VariableName(p.Name)
	if !ok {
		return failure("bad_name")
	}
	seconds, ok := automation.CountdownAddSeconds(p, x.eventAmount())
	if !ok {
		return skipped("below_rate")
	}
	if x.p.automations.Countdowns == nil {
		return failure("no_countdowns")
	}
	result, err := x.p.automations.Countdowns.AddCountdown(x.ctx, x.countdownMutation(s, name, seconds))
	if err != nil {
		if errors.Is(err, ErrVariableLimit) {
			return limited("countdown_limit")
		}
		return failure("store_error")
	}
	if !result.Found {
		return skipped("not_running")
	}
	x.run.CountdownEndMS = result.ExpiresAtMS
	if result.Capped {
		return succeeded("capped")
	}
	return succeeded(strconv.FormatInt(seconds, 10) + "s")
}

func (x *actionExec) countdownMutation(s step, name string, seconds int64) CountdownMutation {
	return CountdownMutation{Ref: CountdownRef{BroadcasterID: x.run.BroadcasterID, Name: name}, Seconds: seconds, OperationID: nestedReplayKey(x.run.replayKey(), s.EffectKey+x.iter)}
}

func (p *Pipeline) startCountdownWorker() {
	if p.automations.Countdowns == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.countdownCancel = cancel
	p.countdownDone = make(chan struct{})
	go p.countdownLoop(ctx)
}

func (p *Pipeline) countdownLoop(ctx context.Context) {
	defer close(p.countdownDone)
	for ctx.Err() == nil {
		if p.pollCountdown(ctx) {
			continue
		}
		if !sleepContext(ctx, time.Second) {
			return
		}
	}
}

func (p *Pipeline) pollCountdown(ctx context.Context) bool {
	call, done := context.WithTimeout(ctx, 10*time.Second)
	due, found, err := p.automations.Countdowns.ClaimCountdown(call)
	done()
	if err != nil {
		p.logCountdownError(ctx, "automations: countdown claim failed", err)
		return false
	}
	if !found {
		return false
	}
	err = p.deliverCountdown(ctx, due)
	if err != nil {
		p.logCountdownError(ctx, "automations: countdown delivery failed", err)
	}
	return err == nil
}

func (p *Pipeline) logCountdownError(ctx context.Context, message string, err error) {
	if ctx.Err() == nil {
		p.log.Warn(message, zap.Error(err))
	}
}

func (p *Pipeline) deliverCountdown(ctx context.Context, due CountdownDue) error {
	call, done := context.WithTimeout(ctx, 45*time.Second)
	defer done()
	if err := p.fireCountdown(call, due); err != nil {
		return err
	}
	return p.automations.Countdowns.AckCountdown(call, due)
}

type countdownDelivery struct {
	p    *Pipeline
	due  CountdownDue
	user projection.User
}

func (p *Pipeline) fireCountdown(ctx context.Context, due CountdownDue) error {
	user, allowed, err := p.countdownChannel(ctx, due.BroadcasterID)
	if err != nil || !allowed {
		return err
	}
	index, err := p.automations.Source.AutomationIndex(ctx, due.BroadcasterID)
	if err != nil {
		return err
	}
	delivery := countdownDelivery{p: p, due: due, user: user}
	for _, trigger := range index.Countdown(due.Name) {
		if err := delivery.run(ctx, trigger.Plan); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pipeline) countdownChannel(ctx context.Context, channel uint64) (projection.User, bool, error) {
	if p.automations.Source == nil {
		return projection.User{}, false, errors.New("no automation source")
	}
	user, err := p.proj.User(ctx, channel)
	if err != nil {
		return user, false, err
	}
	if !p.countdownUserAllowed(user) {
		return user, false, nil
	}
	_, state, err := (ModuleLookup{Proj: p.proj, BroadcasterID: channel, Name: AutomationsModuleName, Absent: ModuleOff}).Resolve(ctx)
	return user, state == ModuleOn, err
}

func (p *Pipeline) countdownUserAllowed(user projection.User) bool {
	return !user.Banned && user.IsActive && (!p.automationsBeta || user.Premium())
}

func (d countdownDelivery) run(ctx context.Context, plan *automation.Plan) error {
	key := countdownProgressKey(plan.Automation.ID, plan.Automation.Revision)
	state := d.due.Progress[key]
	if state == "completed" {
		return nil
	}
	run, err := d.newRun(plan, state)
	if err != nil {
		return err
	}
	run.countdownConditionsPassed = func() error { return d.checkpoint(ctx, key, "conditions_passed") }
	out := d.p.detachedEmit(ctx, d.due.BroadcasterID, d.user.Locale, run.replayKey())
	d.p.runAutomation(ctx, run, out.emit)
	if err := countdownRunError(ctx, run); err != nil {
		return err
	}
	if err := d.p.flushDetached(ctx, out); err != nil {
		return err
	}
	return d.checkpoint(ctx, key, "completed")
}

func (d countdownDelivery) checkpoint(ctx context.Context, key, status string) error {
	return d.p.automations.Countdowns.ProgressCountdown(ctx, d.due, CountdownCheckpoint{Key: key, Status: status})
}

func (d countdownDelivery) newRun(plan *automation.Plan, state string) (*AutomationRun, error) {
	id, err := utils.NewID()
	if err != nil {
		return nil, err
	}
	regress := module.RegressStandard
	if d.user.Premium() {
		regress = module.RegressPremium
	}
	channel := strconv.FormatUint(d.due.BroadcasterID, 10)
	identity := "countdown:" + channel + ":" + d.due.Name + ":" + d.due.Generation
	return &AutomationRun{CountdownAdmission: &countdownAdmission{}, Plan: plan, BroadcasterID: d.due.BroadcasterID, Channel: channel, Viewer: Viewer{ID: d.due.BroadcasterID}, Source: "countdown", ID: id.String(), Identity: identity, Regress: regress, Locale: d.user.Locale, Event: map[string]string{"name": d.due.Name}, countdownSkipConditions: state == "conditions_passed"}, nil
}

func countdownRunError(ctx context.Context, run *AutomationRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if countdownRunFailed(run) {
		return errors.New("countdown run failed")
	}
	if countdownStoreFailed(run) {
		return errors.New("countdown action store error")
	}
	if countdownNotPersisted(run) {
		return errors.New("countdown run not persisted")
	}
	return nil
}

func countdownRunFailed(run *AutomationRun) bool {
	return run.Record.Outcome == automationrpc.OutcomeFailed || run.Record.Reason == queueReasonFull || run.Record.Reason == queueReasonTimeout
}

func countdownStoreFailed(run *AutomationRun) bool {
	for _, action := range run.Record.Actions {
		if action.Detail == "store_error" {
			return true
		}
	}
	return false
}

func countdownNotPersisted(run *AutomationRun) bool {
	admitted := run.CountdownAdmission
	return admitted.failed.Load() || (run.Record.Outcome == outcomeDeferred && !admitted.saved.Load())
}

func (p *Pipeline) answerCountdownLeft(ctx context.Context, c *module.Context, m triggerMatch, emit module.Emit) {
	if p.automations.Countdowns == nil {
		return
	}
	name, ok := automation.VariableName(m.countdownLeft.Name)
	if !ok {
		return
	}
	result, err := p.automations.Countdowns.PeekCountdown(ctx, CountdownRef{BroadcasterID: c.BroadcasterID, Name: name})
	if err != nil {
		p.log.Warn("automations: countdown lookup failed", zap.Error(err))
		return
	}
	p.ensureLocale(ctx, c)
	run := getRun()
	defer putRun(run)
	fillTriggeredRun(run, c, m)
	left := time.Duration(0)
	if result.Found && result.ExpiresAtMS > time.Now().UnixMilli() {
		run.CountdownEndMS = result.ExpiresAtMS
		left = time.Until(time.UnixMilli(result.ExpiresAtMS))
	}
	reply := cmp.Or(m.countdownLeft.ReadyReply, i18n.T(c.Locale, "time.countdown_finished"))
	if left > 0 {
		reply = cmp.Or(m.countdownLeft.LeftReply, i18n.T(c.Locale, "time.countdown_left"))
	}
	x := p.newActionExec(ctx, run, emit)
	defer releaseActionExec(x)
	o := x.out(outgress.TypeChat)
	o.Text = x.expand(reply)
	if x.queueText(o) == nil {
		x.flush()
	}
}

func countdownDuration(locale string, endMS int64) string {
	if endMS == 0 {
		return ""
	}
	return i18n.CompactDuration(locale, time.Duration(endMS-time.Now().UnixMilli())*time.Millisecond)
}

func countdownProgressKey(id string, revision int64) string {
	sum := sha256.Sum256([]byte(id + "\x00" + strconv.FormatInt(revision, 10)))
	return hex.EncodeToString(sum[:])
}
