// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func storedBudget(store ConnectLog, log *zap.Logger, c *budgetClock) *connectBudget {
	b := &connectBudget{sched: defaultBudgetSchedule(), now: c.now, store: store, log: log}
	b.reload()
	return b
}

func TestReloadKeepsLocalAttemptsWhenTheStoreForgets(t *testing.T) {
	store := &fakeConnectLog{}
	c := &budgetClock{t: time.Unix(1_700_000_000, 0)}
	b := storedBudget(store, nil, c)

	b.note()
	store.forget()
	b.reload()

	if st := b.snapshot(); st.Connects != 1 {
		t.Fatalf("connects after the store forgot = %d, want the local attempt kept", st.Connects)
	}
}

func TestReloadDedupesAttemptsItAlreadyHas(t *testing.T) {
	store := &fakeConnectLog{}
	c := &budgetClock{t: time.Unix(1_700_000_000, 0)}
	b := storedBudget(store, nil, c)

	for range 3 {
		b.note()
		c.advance(minConnectInterval)
	}
	b.reload()
	b.reload()

	if st := b.snapshot(); st.Connects != 3 {
		t.Fatalf("connects after two reloads = %d, want the 3 attempts counted once each", st.Connects)
	}
}

func TestDegradedWarningRepeatsOnItsSchedule(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	store := &fakeConnectLog{fail: errors.New("valkey: connection refused")}
	c := &budgetClock{t: time.Unix(1_700_000_000, 0)}

	b := storedBudget(store, zap.New(core), c)
	c.advance(degradedWarnEvery - time.Second)
	b.note()
	if got := logs.FilterLevelExact(zapcore.WarnLevel).Len(); got != 1 {
		t.Fatalf("warns inside the interval = %d, want the boot notice only", got)
	}

	c.advance(2 * time.Second)
	b.note()
	if got := logs.FilterLevelExact(zapcore.WarnLevel).Len(); got != 2 {
		t.Fatalf("warns after %s degraded = %d, want the notice repeated", degradedWarnEvery, got)
	}
}
