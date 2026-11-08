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
type CountdownStore interface {
	StartCountdown(context.Context, uint64, string, int64, string) (CountdownResult, error)
	AddCountdown(context.Context, uint64, string, int64, string) (CountdownResult, error)
	PeekCountdown(context.Context, uint64, string) (CountdownResult, error)
	ClaimCountdown(context.Context) (CountdownDue, bool, error)
	AckCountdown(context.Context, CountdownDue) error
	ProgressCountdown(context.Context, CountdownDue, string, string) error
}

func (x *actionExec) countdownStart(s step, p automation.CountdownStartAction) error {
	name, ok := automation.VariableName(p.Name)
	if !ok {
		return failure("bad_name")
	}
	if x.p.automations.Countdowns == nil {
		return failure("no_countdowns")
	}
	result, err := x.p.automations.Countdowns.StartCountdown(x.ctx, x.run.BroadcasterID, name, int64(p.Seconds), nestedReplayKey(x.run.replayKey(), s.EffectKey+x.iter))
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
	result, err := x.p.automations.Countdowns.AddCountdown(x.ctx, x.run.BroadcasterID, name, seconds, nestedReplayKey(x.run.replayKey(), s.EffectKey+x.iter))
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

func (p *Pipeline) startCountdownWorker() {
	if p.automations.Countdowns == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.countdownCancel = cancel
	p.countdownDone = make(chan struct{})
	go func() {
		defer close(p.countdownDone)
		for ctx.Err() == nil {
			call, done := context.WithTimeout(ctx, 10*time.Second)
			due, found, err := p.automations.Countdowns.ClaimCountdown(call)
			done()
			if err != nil && ctx.Err() == nil {
				p.log.Warn("automations: countdown claim failed", zap.Error(err))
			}
			if err == nil && found {
				call, done = context.WithTimeout(ctx, 45*time.Second)
				err = p.fireCountdown(call, due)
				if err == nil {
					err = p.automations.Countdowns.AckCountdown(call, due)
				}
				done()
				if err != nil && ctx.Err() == nil {
					p.log.Warn("automations: countdown delivery failed", zap.Error(err))
				}
				if err == nil {
					continue
				}
			}
			if !sleepContext(ctx, time.Second) {
				return
			}
		}
	}()
}

func (p *Pipeline) fireCountdown(ctx context.Context, due CountdownDue) error {
	if p.automations.Source == nil {
		return errors.New("no automation source")
	}
	u, err := p.proj.User(ctx, due.BroadcasterID)
	if err != nil {
		return err
	}
	if u.Banned || !u.IsActive {
		return nil
	}
	if p.automationsBeta && !u.Premium() {
		return nil
	}
	regress := module.RegressStandard
	if u.Premium() {
		regress = module.RegressPremium
	}
	_, state, err := (ModuleLookup{Proj: p.proj, BroadcasterID: due.BroadcasterID, Name: AutomationsModuleName, Absent: ModuleOff}).Resolve(ctx)
	if err != nil {
		return err
	}
	if state != ModuleOn {
		return nil
	}
	index, err := p.automations.Source.AutomationIndex(ctx, due.BroadcasterID)
	if err != nil {
		return err
	}
	identity := "countdown:" + strconv.FormatUint(due.BroadcasterID, 10) + ":" + due.Name + ":" + due.Generation
	for _, trigger := range index.Countdown(due.Name) {
		key := countdownProgressKey(trigger.Plan.Automation.ID, trigger.Plan.Automation.Revision)
		state := due.Progress[key]
		if state == "completed" {
			continue
		}
		id, err := utils.NewID()
		if err != nil {
			return err
		}
		admitted := &countdownAdmission{}
		run := &AutomationRun{CountdownAdmission: admitted, Plan: trigger.Plan, BroadcasterID: due.BroadcasterID, Channel: strconv.FormatUint(due.BroadcasterID, 10), Viewer: Viewer{ID: due.BroadcasterID}, Source: "countdown", ID: id.String(), Identity: identity, Regress: regress, Locale: u.Locale, Event: map[string]string{"name": due.Name}}
		run.countdownSkipConditions = state == "conditions_passed"
		run.countdownConditionsPassed = func() error { return p.automations.Countdowns.ProgressCountdown(ctx, due, key, "conditions_passed") }
		out := p.detachedEmit(ctx, due.BroadcasterID, u.Locale, run.replayKey())
		p.runAutomation(ctx, run, out.emit)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if run.Record.Outcome == automationrpc.OutcomeFailed || run.Record.Reason == queueReasonFull || run.Record.Reason == queueReasonTimeout {
			return errors.New("countdown run failed")
		}
		for _, action := range run.Record.Actions {
			if action.Detail == "store_error" {
				return errors.New("countdown action store error")
			}
		}
		if admitted.failed.Load() || (run.Record.Outcome == outcomeDeferred && !admitted.saved.Load()) {
			return errors.New("countdown run not persisted")
		}
		if err := p.flushDetached(ctx, out); err != nil {
			return err
		}
		if err := p.automations.Countdowns.ProgressCountdown(ctx, due, key, "completed"); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pipeline) answerCountdownLeft(ctx context.Context, c *module.Context, m triggerMatch, emit module.Emit) {
	if p.automations.Countdowns == nil {
		return
	}
	name, ok := automation.VariableName(m.countdownLeft.Name)
	if !ok {
		return
	}
	result, err := p.automations.Countdowns.PeekCountdown(ctx, c.BroadcasterID, name)
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
