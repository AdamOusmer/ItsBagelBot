// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"
	"testing"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedExtend answers ExtendLongCooldown with a fixed outcome and counts calls, so a test can prove a redelivery
// did or did not reach the store.
type scriptedExtend struct {
	mu     sync.Mutex
	calls  int
	expiry int64
	found  bool
	capped bool
	err    error
}

func (s *scriptedExtend) ClaimLongCooldowns(context.Context, LongCooldownClaim) (LongCooldownHold, bool, error) {
	return LongCooldownHold{}, false, nil
}

func (s *scriptedExtend) ReleaseLongCooldowns(context.Context, LongCooldownHold) error { return nil }

func (s *scriptedExtend) PeekLongCooldowns(context.Context, LongCooldownClaim) ([]modulesrpc.CooldownWindow, error) {
	return nil, nil
}

func (s *scriptedExtend) ExtendLongCooldown(_ context.Context, _ uint64, _ string, addSeconds int64) (int64, bool, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return 0, false, false, s.err
	}
	if !s.found {
		return 0, false, false, nil
	}
	s.expiry += addSeconds * 1000
	return s.expiry, true, s.capped, nil
}

func (s *scriptedExtend) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func cooldownAddAuto(id string, params obj) automation.Automation {
	return manual(id, act("cooldown_add", params))
}

// eventRun names one direct run: identity is the dedup identity a redelivery would share, runID is unique per
// call so awaitRecord finds the right record, and event injects the fields cooldown_add's per divides by.
type eventRun struct {
	runID    string
	identity string
	auto     automation.Automation
	event    map[string]string
}

// runWithEvent executes an automation directly, bypassing RunNamed, so a test can inject the event fields
// cooldown_add's per divides by without driving the whole event-match pipeline.
func (fx *autoFixture) runWithEvent(t *testing.T, r eventRun) automationrpc.RunRecord {
	t.Helper()
	plan := automation.NewPlan(r.auto)
	run := &AutomationRun{Plan: plan, BroadcasterID: 1, Channel: "1", Viewer: Viewer{ID: 1}, ID: r.runID, Identity: r.identity, Event: r.event}
	out := fx.p.detachedEmit(context.Background(), 1, "", "test:"+r.runID)
	fx.p.runAutomation(context.Background(), run, out.emit)
	require.NoError(t, fx.p.flushDetached(context.Background(), out))
	return fx.awaitRecord(t, r.runID)
}

func TestCooldownAddNotRunningIsSkippedAndCostsOneStoreCall(t *testing.T) {
	fx := newAutoFixture()
	store := &scriptedExtend{found: false}
	fx.p.automations.LongCooldowns = store
	auto := cooldownAddAuto("na", obj{"name": "subathon", "seconds": 30})

	rec := fx.runWithEvent(t, eventRun{runID: "run-na", identity: "id-na", auto: auto})

	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
	require.Len(t, rec.Actions, 1)
	assert.Equal(t, automationrpc.StatusSkipped, rec.Actions[0].Status)
	assert.Equal(t, "not_running", rec.Actions[0].Detail)
	assert.Equal(t, 1, store.callCount())
}

func TestCooldownAddBelowRateIsSkippedWithoutTouchingTheStore(t *testing.T) {
	fx := newAutoFixture()
	store := &scriptedExtend{found: true, expiry: 1_000_000}
	fx.p.automations.LongCooldowns = store
	auto := cooldownAddAuto("br", obj{"name": "subathon", "seconds": 30, "per": 5})

	rec := fx.runWithEvent(t, eventRun{runID: "run-br", identity: "id-br", auto: auto, event: map[string]string{"amount": "4"}})

	require.Len(t, rec.Actions, 1)
	assert.Equal(t, automationrpc.StatusSkipped, rec.Actions[0].Status)
	assert.Equal(t, "below_rate", rec.Actions[0].Detail)
	assert.Equal(t, 0, store.callCount(), "below the rate never reaches the store")
}

func TestCooldownAddSuccessSetsCooldownEndAndDetail(t *testing.T) {
	fx := newAutoFixture()
	start := int64(10_000_000)
	store := &scriptedExtend{found: true, expiry: start}
	fx.p.automations.LongCooldowns = store
	auto := cooldownAddAuto("ok", obj{"name": "subathon", "seconds": 30, "per": 5})

	rec := fx.runWithEvent(t, eventRun{runID: "run-ok", identity: "id-ok", auto: auto, event: map[string]string{"amount": "12"}})

	require.Len(t, rec.Actions, 1)
	assert.Equal(t, automationrpc.StatusOK, rec.Actions[0].Status)
	assert.Equal(t, "60s", rec.Actions[0].Detail, "floor(12/5)=2 lots of 30s, no remainder carried")
	assert.Equal(t, start+60_000, store.expiry)
}

func TestCooldownAddCappedReportsCappedInsteadOfTheDuration(t *testing.T) {
	fx := newAutoFixture()
	store := &scriptedExtend{found: true, expiry: 1, capped: true}
	fx.p.automations.LongCooldowns = store
	auto := cooldownAddAuto("cap", obj{"name": "subathon", "seconds": 30})

	rec := fx.runWithEvent(t, eventRun{runID: "run-cap", identity: "id-cap", auto: auto})

	require.Len(t, rec.Actions, 1)
	assert.Equal(t, automationrpc.StatusOK, rec.Actions[0].Status)
	assert.Equal(t, "capped", rec.Actions[0].Detail)
}

func TestCooldownAddRetainsTerminalAndAmbiguousDecisions(t *testing.T) {
	for _, tc := range []struct {
		name, status, detail string
		err                  error
	}{
		{"store error", automationrpc.StatusFailed, "store_error", assert.AnError},
		{"not running", automationrpc.StatusSkipped, "not_running", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newAutoFixture()
			store := &scriptedExtend{err: tc.err}
			fx.p.automations.LongCooldowns = store
			auto := cooldownAddAuto("decision", obj{"name": "subathon", "seconds": 30})
			first := fx.runWithEvent(t, eventRun{runID: "run-1", identity: "same", auto: auto})
			require.Len(t, first.Actions, 1)
			assert.Equal(t, []string{tc.status, tc.detail}, []string{first.Actions[0].Status, first.Actions[0].Detail})
			store.err, store.found, store.expiry = nil, true, 5_000
			second := fx.runWithEvent(t, eventRun{runID: "run-2", identity: "same", auto: auto})
			require.Len(t, second.Actions, 1)
			assert.Equal(t, "duplicate", second.Actions[0].Detail)
			assert.Equal(t, 1, store.callCount())
		})
	}
}
