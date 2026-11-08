// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package session

import "time"

func (m *Machine) broken(id Connection, now time.Time, effects *Effects) {
	if id == m.pending.id {
		effects.add(Effect{Action: Close, Connection: id})
		m.pending = socket{}
		m.ready = false
		m.scheduleRetry(now, effects)
		return
	}
	effects.add(Effect{Action: Close, Connection: id})
	m.primary = socket{}
	if m.pending.id != 0 {
		effects.add(Effect{Action: Close, Connection: m.pending.id})
		m.pending = socket{}
	}
	m.ready = false
	m.migrating = false
	m.migration = ""
	if m.budgetEnd.IsZero() {
		m.attempts = 0
		m.budgetEnd = now.Add(m.config.RetryBudget)
	}
	m.state = Reconnecting
	m.scheduleRetry(now, effects)
}

func (m *Machine) scheduleRetry(now time.Time, effects *Effects) {
	if m.attempts >= m.config.MaxAttempts || !now.Before(m.budgetEnd) {
		m.exhaust(effects)
		return
	}
	delay := m.config.BackoffBase
	shift := min(max(m.attempts-1, 0), 62)
	if delay > m.config.BackoffMax>>shift {
		delay = m.config.BackoffMax
	} else {
		delay <<= shift
	}
	jitter := max(0, min(m.config.Jitter(delay), delay))
	m.retryAt = now.Add(jitter)
}

func (m *Machine) exhaust(effects *Effects) {
	m.closeSockets(false, effects)
	m.retryAt = time.Time{}
	m.budgetEnd = time.Time{}
	m.migrating = false
	m.migration = ""
	m.ready = false
	if m.primary.bound {
		m.state = Online
		m.problem = MigrationFailed
	} else {
		m.state = Failed
		m.problem = ConnectionFailed
	}
}

func (m *Machine) tick(now time.Time, effects *Effects) {
	if !m.budgetEnd.IsZero() && !now.Before(m.budgetEnd) {
		m.exhaust(effects)
		return
	}
	if expired(m.pending.deadline, now) {
		m.broken(m.pending.id, now, effects)
		return
	}
	if expired(m.primary.deadline, now) {
		m.broken(m.primary.id, now, effects)
		return
	}
	if expired(m.retryAt, now) {
		m.attempt(now, effects)
	}
}

func expired(deadline, now time.Time) bool { return !deadline.IsZero() && !now.Before(deadline) }

func (m *Machine) nextDeadline() time.Time {
	result := m.budgetEnd
	for _, deadline := range [3]time.Time{m.primary.deadline, m.pending.deadline, m.retryAt} {
		if !deadline.IsZero() && (result.IsZero() || deadline.Before(result)) {
			result = deadline
		}
	}
	return result
}

func (m *Machine) closeSockets(all bool, effects *Effects) {
	if m.pending.id != 0 {
		effects.add(Effect{Action: Close, Connection: m.pending.id})
		m.pending = socket{}
	}
	if m.primary.id != 0 && (all || !m.primary.bound) {
		effects.add(Effect{Action: Close, Connection: m.primary.id})
		m.primary = socket{}
	}
}

func (m *Machine) stop(state State, problem Problem, effects *Effects) {
	m.closeSockets(true, effects)
	m.state = state
	m.problem = problem
	m.migrating = false
	m.migration = ""
	m.ready = false
	m.retryAt = time.Time{}
	m.budgetEnd = time.Time{}
	m.cursor = 0
	m.attempts = 0
}
