// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package session

import (
	"testing"
	"time"
)

var epoch = time.Unix(1000, 0)

func testMachine() *Machine {
	return New(Config{BackoffBase: time.Second, BackoffMax: 2 * time.Second, Jitter: func(max time.Duration) time.Duration { return max }})
}
func online(m *Machine, now time.Time) Connection {
	m.Handle(Event{Kind: Start}, now)
	id := m.Snapshot().Primary
	bind(m, id, now)
	return id
}
func bind(m *Machine, id Connection, now time.Time) {
	m.Handle(Event{Kind: Dialed, Connection: id}, now)
	m.Handle(Event{Kind: Welcome, Connection: id, SessionID: "session", Within: time.Second}, now)
	m.Handle(Event{Kind: Bound, Connection: id, SessionID: "session"}, now)
}
func hasEffect(effects Effects, action Action, id Connection) bool {
	for i := 0; i < effects.Count; i++ {
		if effects.Items[i].Action == action && effects.Items[i].Connection == id {
			return true
		}
	}
	return false
}
func TestEmptyConnectThenBind(t *testing.T) {
	m := testMachine()
	effects := m.Handle(Event{Kind: Start}, epoch)
	id := m.Snapshot().Primary
	if !hasEffect(effects, Dial, id) || hasEffect(effects, Bind, id) {
		t.Fatal("initial socket must be empty")
	}
	if m.Route(id, 1) != Reject {
		t.Fatal("unbound socket accepted data")
	}
	m.Handle(Event{Kind: Dialed, Connection: id}, epoch)
	effects = m.Handle(Event{Kind: Welcome, Connection: id, SessionID: "session", Within: time.Second}, epoch)
	if !hasEffect(effects, Bind, id) || m.Snapshot().State != Binding {
		t.Fatal("welcome must trigger account binding")
	}
	m.Handle(Event{Kind: Bound, Connection: id, SessionID: "session"}, epoch)
	if m.Snapshot().State != Online || m.Route(id, 1) != Deliver {
		t.Fatal("binding did not make primary ready")
	}
}
func TestHandoffWaitsForAppliedBoundary(t *testing.T) {
	m := testMachine()
	old := online(m, epoch)
	m.Handle(Event{Kind: Migrate, Connection: old, Migration: "move", Within: 10 * time.Second}, epoch)
	pending := m.Snapshot().Pending
	if pending == 0 || m.Snapshot().Primary != old {
		t.Fatal("migration lost primary")
	}
	bind(m, pending, epoch)
	if m.Route(pending, 3) != Buffer {
		t.Fatal("pending data must remain buffered")
	}
	effects := m.Handle(Event{Kind: Ready, Connection: pending, Migration: "move", Through: 2}, epoch)
	if hasEffect(effects, Close, old) {
		t.Fatal("old socket closed before applied boundary")
	}
	m.Handle(Event{Kind: Applied, Connection: old, Sequence: 1}, epoch)
	effects = m.Handle(Event{Kind: Applied, Connection: old, Sequence: 2}, epoch)
	if !hasEffect(effects, Close, old) || m.Snapshot().Primary != pending || m.Snapshot().Pending != 0 {
		t.Fatal("handoff did not promote then close")
	}
	requireRoute(t, m, pending, 2, Drop)
	requireRoute(t, m, pending, 3, Deliver)
	requireRoute(t, m, pending, 4, Gap)
	before := m.Snapshot()
	m.Handle(Event{Kind: Broken, Connection: old}, epoch)
	if m.Snapshot() != before {
		t.Fatal("retired close changed replacement")
	}
}
func TestPendingFailureRetainsPrimary(t *testing.T) {
	m := testMachine()
	old := online(m, epoch)
	m.Handle(Event{Kind: Migrate, Connection: old, Migration: "move", Within: 10 * time.Second}, epoch)
	failed := m.Snapshot().Pending
	effects := m.Handle(Event{Kind: Broken, Connection: failed}, epoch)
	if hasEffect(effects, Close, old) || !hasEffect(effects, Close, failed) || m.Route(old, 1) != Deliver {
		t.Fatal("candidate failure disrupted primary")
	}
	m.Handle(Event{Kind: Tick}, epoch.Add(time.Second))
	next := m.Snapshot().Pending
	if next == 0 || next == failed {
		t.Fatal("retry reused generation")
	}
	before := m.Snapshot()
	m.Handle(Event{Kind: Ready, Connection: failed, Migration: "move", Through: 0}, epoch.Add(time.Second))
	if before != m.Snapshot() {
		t.Fatal("stale ready promoted old candidate")
	}
}
func TestMigrationDeadlineIsHardLimit(t *testing.T) {
	m := testMachine()
	old := online(m, epoch)
	m.Handle(Event{Kind: Migrate, Connection: old, Migration: "move", Within: time.Second}, epoch)
	pending := m.Snapshot().Pending
	bind(m, pending, epoch)
	effects := m.Handle(Event{Kind: Ready, Connection: pending, Migration: "move", Through: 0}, epoch.Add(time.Second))
	view := m.Snapshot()
	if hasEffect(effects, Close, old) || view.Primary != old || view.Pending != 0 || view.Problem != MigrationFailed {
		t.Fatal("expired migration displaced primary")
	}
}
func TestAttemptBudgetAndExplicitRetry(t *testing.T) {
	m := testMachine()
	now := epoch
	m.Handle(Event{Kind: Start}, now)
	for attempt := 1; attempt <= 5; attempt++ {
		id := m.Snapshot().Primary
		m.Handle(Event{Kind: Broken, Connection: id}, now)
		if attempt < 5 {
			now = now.Add(2 * time.Second)
			m.Handle(Event{Kind: Tick}, now)
		}
	}
	if view := m.Snapshot(); view.State != Failed || view.Attempts != 5 || view.Primary != 0 {
		t.Fatalf("budget not exhausted: %+v", view)
	}
	m.Handle(Event{Kind: Tick}, now.Add(time.Minute))
	if m.Snapshot().State != Failed {
		t.Fatal("automatic retry after terminal failure")
	}
	m.Handle(Event{Kind: Retry}, now.Add(time.Minute))
	if m.Snapshot().Attempts != 1 || m.Snapshot().Primary == 0 {
		t.Fatal("manual retry did not reset budget")
	}
}
func TestRevocationAndStopFenceOldWorkers(t *testing.T) {
	m := testMachine()
	old := online(m, epoch)
	m.Handle(Event{Kind: AuthRejected, Connection: old}, epoch)
	if m.Snapshot().State != AuthenticationRequired || m.Route(old, 1) != Reject {
		t.Fatal("revocation did not stop session")
	}
	m.Handle(Event{Kind: Start}, epoch)
	replacement := m.Snapshot().Primary
	m.Handle(Event{Kind: Stop}, epoch)
	m.Handle(Event{Kind: Dialed, Connection: replacement}, epoch)
	if m.Snapshot().State != Stopped || m.Snapshot().Primary != 0 {
		t.Fatal("late worker revived stopped session")
	}
}
func TestTransitionsDoNotAllocate(t *testing.T) {
	m := testMachine()
	id := online(m, epoch)
	allocations := testing.AllocsPerRun(1000, func() {
		next := m.Snapshot().Cursor + 1
		if m.Route(id, next) != Deliver {
			panic("not deliverable")
		}
		m.Handle(Event{Kind: Applied, Connection: id, Sequence: next}, epoch)
	})
	if allocations != 0 {
		t.Fatalf("hot transition allocated %g times", allocations)
	}
}
func TestActivityWatchdog(t *testing.T) {
	m := New(Config{IdleTimeout: time.Second})
	id := online(m, epoch)
	m.Handle(Event{Kind: Activity, Connection: id}, epoch.Add(500*time.Millisecond))
	m.Handle(Event{Kind: Tick}, epoch.Add(time.Second))
	if m.Snapshot().Primary != id {
		t.Fatal("activity did not extend watchdog")
	}
	m.Handle(Event{Kind: Tick}, epoch.Add(1500*time.Millisecond))
	if m.Snapshot().Primary == id || m.Snapshot().State != Reconnecting {
		t.Fatal("idle socket did not fail")
	}
}

func TestBindingAndMigrationIdentity(t *testing.T) {
	m := testMachine()
	m.Handle(Event{Kind: Start}, epoch)
	id := m.Snapshot().Primary
	m.Handle(Event{Kind: Dialed, Connection: id}, epoch)
	m.Handle(Event{Kind: Welcome, Connection: id, SessionID: "expected", Within: time.Second}, epoch)
	m.Handle(Event{Kind: Bound, Connection: id, SessionID: "wrong"}, epoch)
	if m.Snapshot().State == Online {
		t.Fatal("wrong session identity bound socket")
	}
	m.Handle(Event{Kind: Bound, Connection: id, SessionID: "expected"}, epoch)
	m.Handle(Event{Kind: Migrate, Connection: id, Migration: "move", Within: time.Second}, epoch)
	pending := m.Snapshot().Pending
	bind(m, pending, epoch)
	m.Handle(Event{Kind: Ready, Connection: pending, Migration: "wrong", Through: 0}, epoch)
	if m.Snapshot().Primary != id {
		t.Fatal("wrong migration reference promoted socket")
	}
}

func TestMigrationRetryRequestsFreshReference(t *testing.T) {
	m := testMachine()
	old := online(m, epoch)
	m.Handle(Event{Kind: Migrate, Connection: old, Migration: "old", Within: time.Second}, epoch)
	m.Handle(Event{Kind: Tick}, epoch.Add(time.Second))
	effects := m.Handle(Event{Kind: Retry}, epoch.Add(2*time.Second))
	if !hasEffect(effects, RequestMigration, old) || m.Snapshot().Primary != old || m.Snapshot().Pending != 0 {
		t.Fatal("retry must request a fresh reference without replacing primary")
	}
	m.Handle(Event{Kind: Migrate, Connection: old, Migration: "fresh", Within: time.Second}, epoch.Add(2*time.Second))
	if m.Snapshot().Pending == 0 {
		t.Fatal("fresh instruction did not start replacement")
	}
}

func requireRoute(t *testing.T, m *Machine, id Connection, sequence uint64, want Delivery) {
	t.Helper()
	if got := m.Route(id, sequence); got != want {
		t.Fatalf("route(%d,%d)=%d, want %d", id, sequence, got, want)
	}
}

func TestSuccessiveHandoffsPreserveCursor(t *testing.T) {
	m := testMachine()
	primary := online(m, epoch)
	for move := uint64(1); move <= 2; move++ {
		m.Handle(Event{Kind: Migrate, Connection: primary, Migration: "move", Within: time.Second}, epoch)
		next := m.Snapshot().Pending
		bind(m, next, epoch)
		requireRoute(t, m, next, move, Buffer)
		m.Handle(Event{Kind: Ready, Connection: next, Migration: "move", Through: move - 1}, epoch)
		requireRoute(t, m, next, move, Deliver)
		m.Handle(Event{Kind: Applied, Connection: next, Sequence: move}, epoch)
		if m.Snapshot().Cursor != move {
			t.Fatal("cursor lost across handoff")
		}
		primary = next
	}
}

func TestAppliedCursorCommitsBeforeTimeoutRecovery(t *testing.T) {
	m := New(Config{IdleTimeout: time.Second})
	id := online(m, epoch)
	m.Handle(Event{Kind: Applied, Connection: id, Sequence: 1}, epoch.Add(time.Second))
	if view := m.Snapshot(); view.Cursor != 1 || view.State != Reconnecting {
		t.Fatalf("applied event lost at watchdog boundary: %+v", view)
	}
}

func BenchmarkAppliedTransition(b *testing.B) {
	m := testMachine()
	id := online(m, epoch)
	b.ReportAllocs()
	for b.Loop() {
		m.Handle(Event{Kind: Applied, Connection: id, Sequence: m.Snapshot().Cursor + 1}, epoch)
	}
}

func TestAuthoritativeBindingOutranksTransientControlFailure(t *testing.T) {
	m := testMachine()
	old := online(m, epoch)
	m.Handle(Event{Kind: BindFailed, Connection: old}, epoch)
	if m.Snapshot().State != Online || m.Snapshot().Primary != old {
		t.Fatal("late bind response displaced bound primary")
	}
	m.Handle(Event{Kind: Migrate, Connection: old, Migration: "move", Within: time.Second}, epoch)
	pending := m.Snapshot().Pending
	bind(m, pending, epoch)
	m.Handle(Event{Kind: BindFailed, Connection: pending}, epoch)
	if m.Snapshot().Pending != pending {
		t.Fatal("late bind failure discarded bound candidate")
	}
	m.Handle(Event{Kind: Ready, Connection: pending, Migration: "move", Through: 0}, epoch)
	m.Handle(Event{Kind: BindFailed, Connection: pending}, epoch)
	if m.Snapshot().Primary != pending || m.Snapshot().State != Online {
		t.Fatal("late bind response displaced promoted primary")
	}
	m.Handle(Event{Kind: AuthRejected, Connection: pending}, epoch)
	if m.Snapshot().State != AuthenticationRequired {
		t.Fatal("binding acknowledgement suppressed permanent auth rejection")
	}
}

func TestUnacknowledgedBindFailureStillRetriesCandidate(t *testing.T) {
	m := testMachine()
	old := online(m, epoch)
	m.Handle(Event{Kind: Migrate, Connection: old, Migration: "move", Within: time.Second}, epoch)
	pending := m.Snapshot().Pending
	effects := m.Handle(Event{Kind: BindFailed, Connection: pending}, epoch)
	if !hasEffect(effects, Close, pending) || hasEffect(effects, Close, old) {
		t.Fatal("unacknowledged failure did not isolate candidate")
	}
}

func TestMigrationInstructionOutranksLateRequestFailure(t *testing.T) {
	m := testMachine()
	old := online(m, epoch)
	m.Handle(Event{Kind: MigrationRejected, Connection: old}, epoch)
	m.Handle(Event{Kind: Retry}, epoch)
	m.Handle(Event{Kind: MigrationRequestFailed, Connection: old}, epoch)
	if m.Snapshot().Problem != MigrationFailed || m.Snapshot().Primary != old {
		t.Fatal("unacknowledged request failure disrupted primary")
	}
	m.Handle(Event{Kind: Retry}, epoch)
	m.Handle(Event{Kind: Migrate, Connection: old, Migration: "fresh", Within: time.Second}, epoch)
	pending := m.Snapshot().Pending
	m.Handle(Event{Kind: MigrationRequestFailed, Connection: old}, epoch)
	if m.Snapshot().Pending != pending || m.Snapshot().State != Migrating {
		t.Fatal("late request response aborted accepted migration")
	}
}
