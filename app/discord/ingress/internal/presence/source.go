// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package presence computes the Discord "Watching N streams" activity name
// discord-ingress's gateway session shows, from the users service's narrow
// counts RPC. It implements gateway.PresenceSource; see that interface's doc
// for why sending the status itself is gateway's job, not this package's.
package presence

import (
	"context"
	"sync"

	"go.uber.org/zap"
)

// Fetch returns the current total enrolled-user count. In production this
// calls the users service's bagel.rpc.internal.users.counts.get (see
// NewFetch) -- never bagel.rpc.admin.user.stats, the console's admin-gated
// surface: that RPC also reports premium/paid/VIP breakdowns, and handing an
// internet-facing gateway pod a query onto that data for a cosmetic status
// line would be a needless widening of what a compromised or merely buggy
// ingress process could leak. Tests supply a fake Fetch instead of a live
// NATS round trip.
type Fetch func(ctx context.Context) (total int, err error)

// Source turns Fetch into a gateway.PresenceSource: it formats the count as
// an activity name and only reports a send when that name has changed since
// the last one it handed out.
type Source struct {
	Fetch Fetch
	Log   *zap.Logger

	mu   sync.Mutex
	last string
}

// Refresh calls Fetch and reports the activity name to send, or ok=false
// when nothing should go out: Fetch failed (logged and swallowed -- see
// gateway.PresenceSource's doc on why this must never surface as an error),
// or the formatted name matches the last one this Source reported.
func (s *Source) Refresh(ctx context.Context) (string, bool) {
	total, err := s.Fetch(ctx)
	if err != nil {
		s.log().Warn("discord presence count fetch failed", zap.Error(err))
		return "", false
	}

	name := activityName(total)
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == s.last {
		return "", false
	}
	s.last = name
	return name, true
}

// Forget clears the dedup state so the next Refresh reports ok=true even if
// the count has not moved. gateway.Session calls this once per fresh
// connection: presence does not survive a reconnect (a new IDENTIFY starts
// with no activity), so the unchanged-count case must still go out again.
func (s *Source) Forget() {
	s.mu.Lock()
	s.last = ""
	s.mu.Unlock()
}

func (s *Source) log() *zap.Logger {
	if s.Log != nil {
		return s.Log
	}
	return zap.NewNop()
}
