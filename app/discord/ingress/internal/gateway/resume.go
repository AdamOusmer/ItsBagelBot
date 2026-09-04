// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import "sync"

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
