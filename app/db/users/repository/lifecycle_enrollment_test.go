// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"testing"
	"time"

	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	"github.com/stretchr/testify/require"
)

func servingState(runtimeState string) lifecycle.State {
	return lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: runtimeState, AdmissionGeneration: 4, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy}
}

func TestLifecycleEnrollRetryBacksOffOnlyTheActiveGeneration(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name       string
		state      lifecycle.State
		generation int64
		wantState  string
	}{
		{name: "active generation enters bounded recovery", state: servingState(lifecycle.Active), generation: 4, wantState: lifecycle.Recovering},
		{name: "stale generation is ignored", state: servingState(lifecycle.Active), generation: 3, wantState: lifecycle.Active},
		{name: "degraded channel keeps its retry delay", state: servingState(lifecycle.Degraded), generation: 4, wantState: lifecycle.Degraded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.state
			changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "enroll_retry", AdmissionGeneration: tc.generation}, now, false)
			require.NoError(t, err)
			require.Equal(t, tc.wantState != tc.state.RuntimeState, changed)
			require.Equal(t, tc.wantState, s.RuntimeState)
		})
	}
}

func TestLifecycleMissingScopeNoticeGivesWayToABanNotice(t *testing.T) {
	now := time.Now().UTC()
	s := servingState(lifecycle.Degraded)

	changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "scope_missing"}, now, false)
	require.NoError(t, err)
	require.True(t, changed)
	scopeIncident := s.IncidentID
	require.NotEmpty(t, scopeIncident)
	require.Equal(t, lifecycle.ReasonScopeMissing, s.IncidentReason)
	again, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "scope_missing"}, now, false)
	require.NoError(t, err)
	require.False(t, again, "an open incident is not replaced by a second scope request")

	s.IncidentAnnounced = true
	_, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "ban", EventID: "ban", ObservedAt: now}, now, false)
	require.NoError(t, err)
	type incident struct {
		reason       string
		replaced     bool
		announced    bool
		retractionOf string
	}
	got := incident{reason: s.IncidentReason, replaced: s.IncidentID != scopeIncident, announced: s.IncidentAnnounced, retractionOf: s.RetractionIncidentID}
	require.Equal(t, incident{reason: "bot_banned", replaced: true, retractionOf: scopeIncident}, got, "the owner hears about the ban, not only the scope")
}
