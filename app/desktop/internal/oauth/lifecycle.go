// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package oauth

import "context"

type lifecycleNotice struct {
	ready      bool
	generation uint64
	ack        chan struct{}
	result     chan error
}

// Lifecycle callbacks run outside token operations. A token provider returns
// before a callback joins the event worker that requested that token.
func (m *Manager) lifecycle() {
	defer m.workers.Done()
	for {
		select {
		case n := <-m.notifications:
			m.applyLifecycle(n)
		case <-m.ctx.Done():
			return
		}
	}
}

func (m *Manager) applyLifecycle(n lifecycleNotice) {
	if n.ack != nil {
		defer close(n.ack)
	}
	err := m.performLifecycle(n)
	if n.result != nil {
		n.result <- err
	}
}

func (m *Manager) performLifecycle(n lifecycleNotice) error {
	m.mu.Lock()
	valid := m.generation == n.generation && !m.closed
	signedIn := m.status.SignedIn
	m.mu.Unlock()
	if !valid {
		return ErrStopped
	}
	if !n.ready && m.config.SignedOut != nil {
		return m.config.SignedOut()
	}
	if n.ready && signedIn && m.config.Ready != nil {
		return m.config.Ready(m.ctx, m)
	}
	return nil
}

func (m *Manager) notify(n lifecycleNotice) {
	select {
	case m.notifications <- n:
	case <-m.ctx.Done():
	}
}

func (m *Manager) stopSession(ctx context.Context, generation uint64) error {
	result := make(chan error, 1)
	m.notify(lifecycleNotice{generation: generation, result: result})
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ErrStopped
	}
}
