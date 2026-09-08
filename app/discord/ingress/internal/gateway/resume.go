// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"sync"
	"time"
)

// resumeState is what makes a reconnect continue a session instead of
// starting a new one.
//
// Without it, every dropped socket re-Identifies, and Discord treats that as
// a brand new session: everything it buffered during the gap is discarded,
// silently, with no error and no gap marker. That was tolerable when this bot
// only posted welcomes. It is not tolerable now that the engine runs an
// automod: the events lost in that window are exactly the MESSAGE_CREATEs
// linkguard inspects and the GUILD_MEMBER_ADDs raid detection counts, and a
// gateway blip during a raid is precisely when losing them matters most.
//
// Resuming needs three things, all tracked here: the session id and resume
// URL Discord hands back in READY, and the sequence number of the last event
// actually received. The sequence is also what the heartbeat must carry --
// sending a null there tells Discord "I have seen nothing", which defeats its
// own missed-event detection even while the socket stays up.
//
// Guarded by a mutex because the heartbeat goroutine reads the sequence while
// the pump goroutine writes it.
type resumeState struct {
	mu        sync.Mutex
	sessionID string
	resumeURL string
	seq       *int
	// upAt is when the current connection reached READY or RESUMED, zero
	// until it does. It is the clock behind the backoff reset (see
	// reconnect.stableFor), and it lives here rather than in a type of its
	// own because it is written from exactly the two functions that already
	// hold this state -- readyFrom and the RESUMED branch of onDispatch --
	// while a second pointer would have to be threaded through every
	// function in the pump chain for one time.Time.
	upAt time.Time
	// opened is which of op 2 and op 6 the connection in flight sent, empty
	// until Hello. It rides here rather than on Session because Session is
	// copied by value into every method and this changes per connection.
	opened openMode
	// resumedUp records that RESUMED landed rather than READY. With opened
	// it is the whole answer to "did the resume take": opened=resume and
	// resumedUp=false is a resume Discord discarded, which makes the next
	// connect spend one IDENTIFY out of 1000/day.
	resumedUp bool
	// lastID is the session id most recently in play, deliberately kept
	// across invalidate(). sessionID above is what a resume may use and must
	// be cleared when Discord refuses it; this one is read by telemetry only
	// and never sent, so a socket-end line can still name the session that
	// died rather than logging an empty string for every refused resume.
	lastID string
}

// sessionMark is what a finished socket's log line needs from the state: how
// the connection opened, whether the resume took, and which session it was.
// One value rather than three same-shaped accessors, since all three come off
// the same lock and three sibling one-liners is what CodeScene reads as
// duplication.
type sessionMark struct {
	opened    openMode
	resumed   bool
	sessionID string
}

// resetUp clears the per-connection state at the start of an attempt. The
// session identity (sessionID, resumeURL, seq, lastID) deliberately survives:
// it belongs to the session, not to the socket carrying it.
func (r *resumeState) resetUp() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upAt = time.Time{}
	r.opened = ""
	r.resumedUp = false
}

// markUp records that this connection reached READY or RESUMED. The first
// one wins: a socket that resumes and then receives a second RESUMED is
// still the same connection, and restamping would understate its uptime.
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

// markOpened records which opcode this connection sent after Hello.
func (r *resumeState) markOpened(mode openMode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.opened = mode
}

// mark reports the connection's telemetry. See sessionMark.
func (r *resumeState) mark() sessionMark {
	r.mu.Lock()
	defer r.mu.Unlock()
	return sessionMark{opened: r.opened, resumed: r.resumedUp, sessionID: r.lastID}
}

// upFor reports how long this connection has been up, or zero if it never
// reached READY/RESUMED -- which is exactly what a dial that hung or a
// socket that died mid-handshake earned.
func (r *resumeState) upFor(now time.Time) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.upAt.IsZero() {
		return 0
	}
	return now.Sub(r.upAt)
}

// note records the sequence of a received packet. Discord sends s only on
// dispatch payloads, so a nil is normal and must not clear what we have.
func (r *resumeState) note(s *int) {
	if s == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v := *s
	r.seq = &v
}

// ready records the session identity from a READY payload.
func (r *resumeState) ready(sessionID, resumeURL string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionID = sessionID
	r.resumeURL = resumeURL
	r.lastID = sessionID
}

// sequence returns a copy of the last seen sequence, for the heartbeat.
func (r *resumeState) sequence() *int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seq == nil {
		return nil
	}
	v := *r.seq
	return &v
}

// resumable reports the session id and URL to resume with, and whether a
// resume should be attempted at all.
func (r *resumeState) resumable() (sessionID, resumeURL string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessionID == "" {
		return "", "", false
	}
	return r.sessionID, r.resumeURL, true
}

// invalidate drops the session so the next connect Identifies fresh. Called
// when Discord says the session cannot be resumed (INVALID_SESSION with
// d:false), and after a resume attempt is itself rejected -- retrying a
// resume Discord already refused just loops.
//
// The sequence is cleared with it: a sequence belongs to one session, and
// carrying it into a new one would have the heartbeat claim events this
// session never saw.
func (r *resumeState) invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionID = ""
	r.resumeURL = ""
	r.seq = nil
}
