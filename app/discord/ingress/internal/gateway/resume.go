// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"sync"
	"time"
)

type resumeState struct {
	mu        sync.Mutex
	sessionID string
	resumeURL string
	seq       *int
	upAt      time.Time
	opened    openMode
	resumedUp bool
	lastID    string
}

type sessionMark struct {
	opened    openMode
	resumed   bool
	sessionID string
}

func (r *resumeState) resetUp() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upAt = time.Time{}
	r.opened = ""
	r.resumedUp = false
}

func (r *resumeState) markUp(now time.Time, resumed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if resumed {
		r.resumedUp = true
	}
	if r.upAt.IsZero() {
		r.upAt = now
	}
}

func (r *resumeState) markOpened(mode openMode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.opened = mode
}

func (r *resumeState) mark() sessionMark {
	r.mu.Lock()
	defer r.mu.Unlock()
	return sessionMark{opened: r.opened, resumed: r.resumedUp, sessionID: r.lastID}
}

func (r *resumeState) upFor(now time.Time) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.upAt.IsZero() {
		return 0
	}
	return now.Sub(r.upAt)
}

func (r *resumeState) note(s *int) {
	if s == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v := *s
	r.seq = &v
}

func (r *resumeState) ready(sessionID, resumeURL string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionID = sessionID
	r.resumeURL = resumeURL
	r.lastID = sessionID
}

func (r *resumeState) sequence() *int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seq == nil {
		return nil
	}
	v := *r.seq
	return &v
}

func (r *resumeState) resumable() (sessionID, resumeURL string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessionID == "" {
		return "", "", false
	}
	return r.sessionID, r.resumeURL, true
}

func (r *resumeState) invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionID = ""
	r.resumeURL = ""
	r.seq = nil
}
