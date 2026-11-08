// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package session

import (
	"math/rand/v2"
	"time"
)

type socket struct {
	id        Connection
	session   string
	deadline  time.Time
	bound     bool
	connected bool
}

type Machine struct {
	config    Config
	state     State
	problem   Problem
	primary   socket
	pending   socket
	next      Connection
	cursor    uint64
	attempts  int
	budgetEnd time.Time
	retryAt   time.Time
	migration string
	migrating bool
	ready     bool
	through   uint64
}

func New(config Config) *Machine {
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 45 * time.Second
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 5
	}
	if config.RetryBudget <= 0 {
		config.RetryBudget = 30 * time.Second
	}
	if config.DialTimeout <= 0 {
		config.DialTimeout = 5 * time.Second
	}
	if config.WelcomeTimeout <= 0 {
		config.WelcomeTimeout = 5 * time.Second
	}
	if config.BindTimeout <= 0 {
		config.BindTimeout = 5 * time.Second
	}
	if config.BackoffBase <= 0 {
		config.BackoffBase = 250 * time.Millisecond
	}
	if config.BackoffMax <= 0 {
		config.BackoffMax = 5 * time.Second
	}
	if config.Jitter == nil {
		config.Jitter = fullJitter
	}
	return &Machine{config: config}
}

func fullJitter(max time.Duration) time.Duration {
	return time.Duration(rand.Uint64N(uint64(max)))
}

func (m *Machine) Snapshot() View {
	return View{State: m.state, Problem: m.problem, Primary: m.primary.id, Pending: m.pending.id, Cursor: m.cursor, Attempts: m.attempts}
}

func (m *Machine) Handle(event Event, now time.Time) Effects {
	var effects Effects
	before := m.Snapshot()
	if event.Kind != Tick && event.Kind != Stop && event.Kind != Applied {
		m.tick(now, &effects)
	}
	m.handle(event, now, &effects)
	if event.Kind == Applied {
		m.tick(now, &effects)
	}
	if before != m.Snapshot() {
		effects.add(Effect{Action: Changed})
	}
	effects.add(Effect{Action: ArmTimer, At: m.nextDeadline()})
	return effects
}

func (m *Machine) handle(event Event, now time.Time, effects *Effects) {
	switch event.Kind {
	case Start:
		m.start(now, effects)
	case Retry:
		m.retry(now, effects)
	case Stop:
		m.stop(Stopped, NoProblem, effects)
	case Tick:
		m.tick(now, effects)
	default:
		m.socketEvent(event, now, effects)
	}
}

func (m *Machine) socketEvent(event Event, now time.Time, effects *Effects) {
	target := m.find(event.Connection)
	if target == nil {
		return
	}
	switch event.Kind {
	case Dialed:
		if !target.connected && !target.bound {
			target.connected = true
			target.deadline = now.Add(m.config.WelcomeTimeout)
		}
	case Activity:
		if target.bound {
			target.deadline = now.Add(m.config.IdleTimeout)
		}
	case Welcome:
		m.welcome(target, event, now, effects)
	case Bound:
		m.bound(target, event, now, effects)
	default:
		m.controlEvent(event, now, effects)
	}
}

func (m *Machine) controlEvent(event Event, now time.Time, effects *Effects) {
	switch event.Kind {
	case MigrationRejected:
		if event.Connection == m.primary.id && m.primary.bound {
			m.exhaust(effects)
		}
	case Migrate:
		m.migrate(event, now, effects)
	case Ready:
		m.handoff(event, effects)
	case Applied:
		m.applied(event, effects)
	case BindFailed, MigrationRequestFailed:
		m.controlFailed(event, now, effects)
	case Broken:
		m.broken(event.Connection, now, effects)
	case AuthRejected:
		m.stop(AuthenticationRequired, Unauthorized, effects)
	}
}

func (m *Machine) find(id Connection) *socket {
	if id == 0 {
		return nil
	}
	if id == m.primary.id {
		return &m.primary
	}
	if id == m.pending.id {
		return &m.pending
	}
	return nil
}

func (m *Machine) start(now time.Time, effects *Effects) {
	if m.primary.bound || m.pending.id != 0 || m.primary.id != 0 {
		return
	}
	m.problem = NoProblem
	m.migrating = false
	m.migration = ""
	m.attempts = 0
	m.budgetEnd = now.Add(m.config.RetryBudget)
	m.attempt(now, effects)
}

func (m *Machine) attempt(now time.Time, effects *Effects) {
	if m.attempts >= m.config.MaxAttempts || !now.Before(m.budgetEnd) {
		m.exhaust(effects)
		return
	}
	m.retryAt = time.Time{}
	m.attempts++
	m.next++
	target := socket{id: m.next, deadline: now.Add(m.config.DialTimeout)}
	if m.migrating {
		m.pending = target
		m.state = Migrating
	} else {
		m.primary = target
		m.state = Connecting
	}
	effects.add(Effect{Action: Dial, Connection: target.id, At: target.deadline})
}

func (m *Machine) welcome(target *socket, event Event, now time.Time, effects *Effects) {
	if !target.connected || target.bound || target.session != "" || event.SessionID == "" || event.Within <= 0 {
		m.broken(target.id, now, effects)
		return
	}
	target.session = event.SessionID
	target.deadline = now.Add(min(event.Within, m.config.BindTimeout))
	if !m.migrating {
		m.state = Binding
	}
	effects.add(Effect{Action: Bind, Connection: target.id, SessionID: target.session, Migration: m.migration, Cursor: m.cursor, At: target.deadline})
}

func (m *Machine) bound(target *socket, event Event, now time.Time, effects *Effects) {
	if event.SessionID != target.session {
		return
	}
	if target.session == "" || target.bound {
		return
	}
	target.bound = true
	target.deadline = now.Add(m.config.IdleTimeout)
	if target == &m.primary {
		m.state = Online
		m.problem = NoProblem
		m.budgetEnd = time.Time{}
		m.attempts = 0
	}
}

func (m *Machine) migrate(event Event, now time.Time, effects *Effects) {
	if !m.CanMigrate(event.Connection) {
		return
	}
	if event.Migration == "" || event.Within <= 0 {
		return
	}
	m.migrating = true
	m.migration = event.Migration
	m.ready = false
	m.problem = NoProblem
	m.attempts = 0
	m.budgetEnd = now.Add(min(event.Within, m.config.RetryBudget))
	m.attempt(now, effects)
}

func (m *Machine) handoff(event Event, effects *Effects) {
	if event.Connection != m.pending.id || !m.pending.bound || !m.migrating || event.Migration != m.migration {
		return
	}
	m.ready = true
	m.through = event.Through
	m.promote(effects)
}

func (m *Machine) applied(event Event, effects *Effects) {
	if !m.primary.bound || event.Connection != m.primary.id || event.Sequence != m.cursor+1 {
		return
	}
	m.cursor = event.Sequence
	m.promote(effects)
}

func (m *Machine) promote(effects *Effects) {
	if !m.ready || m.cursor < m.through {
		return
	}
	effects.add(Effect{Action: Close, Connection: m.primary.id})
	m.primary = m.pending
	m.pending = socket{}
	m.ready = false
	m.migrating = false
	m.migration = ""
	m.state = Online
	m.problem = NoProblem
	m.budgetEnd = time.Time{}
	m.attempts = 0
}

func (m *Machine) Route(id Connection, sequence uint64) Delivery {
	target := m.find(id)
	if target == nil || !target.bound {
		return Reject
	}
	if sequence <= m.cursor {
		return Drop
	}
	if id == m.pending.id {
		return Buffer
	}
	if sequence != m.cursor+1 {
		return Gap
	}
	if m.ready && sequence > m.through {
		return Gap
	}
	return Deliver
}

// Retry a failed migration by requesting a fresh server-owned reference. Never
// reuse an expired ticket or close a healthy primary merely to force rebalance.
func (m *Machine) retry(now time.Time, effects *Effects) {
	if m.primary.bound && m.problem == MigrationFailed {
		m.problem = NoProblem
		m.state = Migrating
		m.budgetEnd = now.Add(m.config.RetryBudget)
		effects.add(Effect{Action: RequestMigration, Connection: m.primary.id, Cursor: m.cursor, At: m.budgetEnd})
		return
	}
	m.start(now, effects)
}

// CanMigrate prevents duplicate instructions from replacing an in-flight ticket
// or target while its retry budget is still owned by the supervisor.
func (m *Machine) CanMigrate(id Connection) bool {
	return id == m.primary.id && m.primary.bound && !m.migrating
}

// Socket acknowledgements outrank a late transient control response. Permanent
// authentication rejection is handled separately and always stops the session.
func (m *Machine) controlFailed(event Event, now time.Time, effects *Effects) {
	target := m.find(event.Connection)
	if event.Kind == BindFailed {
		if !target.bound {
			m.broken(event.Connection, now, effects)
		}
		return
	}
	if target == &m.primary && target.bound && !m.migrating {
		m.exhaust(effects)
	}
}
