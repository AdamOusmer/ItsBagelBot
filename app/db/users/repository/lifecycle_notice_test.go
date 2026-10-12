package repository

import (
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLifecycleUndeliveredNoticeExpiresAtDeadline(t *testing.T) {
	now := time.Now().UTC()
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, AdmissionGeneration: 1, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy}
	_, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "grant_missing", EventID: "revoke", ObservedAt: now, AdmissionGeneration: s.AdmissionGeneration}, now, false)
	require.NoError(t, err)
	require.NotEmpty(t, s.IncidentID)
	changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "notice_claim"}, s.NoticeDeadline, false)
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, s.IncidentAnnounced)
	require.True(t, s.NoticeNextAttemptAt.IsZero(), "an undeliverable notice must leave the due set")
}
