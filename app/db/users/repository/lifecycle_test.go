package repository

import (
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	"context"
	entcore "entgo.io/ent"
	entsql "entgo.io/ent/dialect/sql"
	"errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync/atomic"
	"testing"
	"time"
)

func TestLifecycleBanUnbanDisableAndStaleRecovery(t *testing.T) {
	client, repo := lifecycleSetup(t)
	defer repo.Close(context.Background())
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, 1001, "mavey", "Mavey", "mavey@example.com"))
	row := client.User.GetX(ctx, 1001)
	inc := row.CreatedAt.UnixMicro()
	now := time.Now().UTC()
	ban, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{UserID: 1001, AccountCreatedAt: inc, Action: "ban", EventID: "ban", ObservedAt: now})
	require.NoError(t, err)
	require.True(t, ban.Applied)
	require.Equal(t, lifecycle.Blocked, ban.State.RuntimeState)
	require.True(t, ban.State.DesiredEnabled)
	require.False(t, client.User.GetX(ctx, 1001).IsActive)
	duplicate, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{UserID: 1001, AccountCreatedAt: inc, Action: "ban", EventID: "ban", ObservedAt: now})
	require.NoError(t, err)
	require.False(t, duplicate.Applied)
	require.Equal(t, ban.State.IncidentID, duplicate.State.IncidentID)
	unban, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{UserID: 1001, AccountCreatedAt: inc, Action: "unban", EventID: "unban", ObservedAt: now.Add(time.Second)})
	require.NoError(t, err)
	require.Equal(t, lifecycle.Recovering, unban.State.RuntimeState)
	require.False(t, unban.State.AllowsRuntime())
	require.NoError(t, repo.SetActiveNow(ctx, 1001, false))
	stale, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{UserID: 1001, AccountCreatedAt: inc, Action: "recovery_success", OperationID: unban.State.OperationID, AdmissionGeneration: unban.State.AdmissionGeneration})
	require.NoError(t, err)
	require.False(t, stale.Applied)
	require.False(t, stale.State.DesiredEnabled)
	require.Equal(t, lifecycle.Disabled, stale.State.RuntimeState)
	require.Equal(t, ban.State.IncidentID, stale.State.IncidentID, "owner disable retains suppression")
}

func TestLifecycleModerationOrderingAndIndependentGrant(t *testing.T) {
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, AdmissionGeneration: 1, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy}
	now := time.Now().UTC()
	req := usersrpc.LifecycleRequest{Action: "unban", EventID: "new", ObservedAt: now}
	changed, err := transitionLifecycle(&s, req, now, false)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "ban", EventID: "old", ObservedAt: now.Add(-time.Second)}, now, false)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, lifecycle.RestrictionNone, s.ChatRestriction)
	_, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "ban", EventID: "ban", ObservedAt: now.Add(time.Second)}, now, false)
	require.NoError(t, err)
	_, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "grant_missing", EventID: "grant-revoked", ObservedAt: now.Add(time.Second), AdmissionGeneration: s.AdmissionGeneration}, now, false)
	require.NoError(t, err)
	_, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "unban", EventID: "unban", ObservedAt: now.Add(2 * time.Second)}, now, false)
	require.NoError(t, err)
	require.Equal(t, lifecycle.GrantMissing, s.GrantHealth)
	require.Equal(t, lifecycle.Blocked, s.RuntimeState)
	require.False(t, s.AllowsRuntime())
}

func TestLifecycleDormancyWakeAndBoundedRecovery(t *testing.T) {
	now := time.Now().UTC()
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, AdmissionGeneration: 1, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy, LastEngagedAt: now.Add(-lifecycle.DormancyPeriod)}
	changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "dormancy", Reason: "offline", AdmissionGeneration: s.AdmissionGeneration}, now, false)
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, s.AllowsRuntime())
	require.False(t, s.AllowsBackground())
	generation := s.AdmissionGeneration
	changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "engage", EventID: "command", ObservedAt: now, AdmissionGeneration: generation}, now, false)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, lifecycle.Active, s.RuntimeState)
	changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "engage", EventID: "command", ObservedAt: now, AdmissionGeneration: generation}, now, false)
	require.NoError(t, err)
	require.False(t, changed)
	_, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "retry"}, now, false)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		req := usersrpc.LifecycleRequest{Action: "claim", OperationID: s.OperationID, AdmissionGeneration: s.AdmissionGeneration}
		changed, err = transitionLifecycle(&s, req, now, false)
		require.NoError(t, err)
		require.True(t, changed)
		req.Action = "recovery_failed"
		_, err = transitionLifecycle(&s, req, now, false)
		require.NoError(t, err)
		now = now.Add(10 * time.Minute)
	}
	require.Equal(t, lifecycle.Degraded, s.RuntimeState)
	require.False(t, s.AllowsRuntime())
	changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "degraded_retry"}, now, false)
	require.NoError(t, err)
	require.False(t, changed, "degraded channels wait out the retry delay")
	changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "degraded_retry"}, s.NextAttemptAt, false)
	require.NoError(t, err)
	require.True(t, changed, "degraded channels retry automatically after a transient failure")
	require.Equal(t, lifecycle.Recovering, s.RuntimeState)
	require.Empty(t, s.IncidentID, "transport failures must not create incidents")
}

func TestLifecycleIncarnationAndDueOutbox(t *testing.T) {
	client, repo := lifecycleSetup(t)
	defer repo.Close(context.Background())
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, 1001, "mavey", "Mavey", "mavey@example.com"))
	row := client.User.GetX(ctx, 1001)
	inc := row.CreatedAt.UnixMicro()
	wrong, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{UserID: 1001, AccountCreatedAt: inc - 1, Action: "ban", EventID: "old", ObservedAt: time.Now()})
	require.NoError(t, err)
	require.False(t, wrong.Applied)
	ban, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{UserID: 1001, AccountCreatedAt: inc, Action: "ban", EventID: "ban", ObservedAt: time.Now()})
	require.NoError(t, err)
	require.False(t, ban.State.NoticeDeadline.IsZero())
	due, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "due"})
	require.NoError(t, err)
	require.Len(t, due.Items, 1)
	require.Equal(t, ban.State.IncidentID, due.Items[0].State.IncidentID)
	ack, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{UserID: 1001, AccountCreatedAt: inc, Action: "notice_ack", IncidentID: ban.State.IncidentID})
	require.NoError(t, err)
	require.True(t, ack.State.IncidentAnnounced)
	due, err = repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "due"})
	require.NoError(t, err)
	require.Empty(t, due.Items)
}

func lifecycleSetup(t *testing.T) (*ent.Client, *Users) {
	t.Helper()
	driver, err := entsql.Open(testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	require.NoError(t, err)
	driver.DB().SetMaxOpenConns(1)
	client := ent.NewClient(ent.Driver(driver))
	require.NoError(t, client.Schema.Create(t.Context()))
	t.Cleanup(func() { _ = client.Close() })
	return client, NewUsers(client, nil, bustest.NewPublisher(), nil, zap.NewNop())
}

func TestLifecycleErasureSurvivesRecreation(t *testing.T) {
	client, repo := lifecycleSetup(t)
	defer repo.Close(t.Context())
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, 1001, "mavey", "Mavey", "mavey@example.com"))
	old := client.User.GetX(ctx, 1001).CreatedAt.UnixMicro()
	require.NoError(t, repo.Delete(ctx, 1001))
	require.NoError(t, repo.Register(ctx, 1001, "mavey", "Mavey", "mavey@example.com"))
	replacement := client.User.GetX(ctx, 1001).CreatedAt.UnixMicro()
	require.NotEqual(t, old, replacement)
	due, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "due"})
	require.NoError(t, err)
	require.Len(t, due.Erasures, 1)
	require.Equal(t, old, due.Erasures[0].AccountCreatedAt)
	claimed, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "erase_claim", UserID: 1001, AccountCreatedAt: old})
	require.NoError(t, err)
	require.True(t, claimed.Applied)
	duplicate, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "erase_claim", UserID: 1001, AccountCreatedAt: old})
	require.NoError(t, err)
	require.False(t, duplicate.Applied)
	for _, reason := range []string{"notifications", "outgress"} {
		_, err = repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "erase_ack", UserID: 1001, AccountCreatedAt: old, Reason: reason})
		require.NoError(t, err)
	}
	require.Equal(t, replacement, client.User.GetX(ctx, 1001).CreatedAt.UnixMicro())
	due, err = repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "due"})
	require.NoError(t, err)
	require.Empty(t, due.Erasures)
}

func TestLifecycleConcurrentBanAndDisable(t *testing.T) {
	client, repo := lifecycleSetup(t)
	defer repo.Close(t.Context())
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, 1001, "mavey", "Mavey", "mavey@example.com"))
	inc := client.User.GetX(ctx, 1001).CreatedAt.UnixMicro()
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() { <-start; results <- repo.SetActiveNow(ctx, 1001, false) }()
	go func() {
		<-start
		_, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{UserID: 1001, AccountCreatedAt: inc, Action: "ban", EventID: "ban", ObservedAt: time.Now()})
		results <- err
	}()
	close(start)
	for i := 0; i < 2; i++ {
		require.NoError(t, <-results)
	}
	row := client.User.GetX(ctx, 1001)
	require.False(t, row.IsActive)
	require.False(t, row.Lifecycle.DesiredEnabled)
	require.Equal(t, lifecycle.Disabled, row.Lifecycle.RuntimeState)
	require.Equal(t, lifecycle.RestrictionBanned, row.Lifecycle.ChatRestriction)
}

type failingLifecyclePublisher struct{ *bustest.Publisher }

func (p failingLifecyclePublisher) PublishOwned(context.Context, string, []byte) error {
	return errors.New("publication unavailable")
}
func (p failingLifecyclePublisher) PublishOwnedWithID(context.Context, string, string, []byte) error {
	return errors.New("publication unavailable")
}
func TestLifecyclePublicationRetryPreservesRevision(t *testing.T) {
	client, repo := lifecycleSetup(t)
	defer repo.Close(t.Context())
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, 1001, "mavey", "Mavey", "mavey@example.com"))
	repo.pub = failingLifecyclePublisher{bustest.NewPublisher()}
	require.Error(t, repo.SetActiveNow(ctx, 1001, false))
	row := client.User.GetX(ctx, 1001)
	require.False(t, row.IsActive)
	require.True(t, row.LifecycleProjectionPending)
	revision := row.StateRevision
	repo.pub = bustest.NewPublisher()
	_, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "due"})
	require.NoError(t, err)
	row = client.User.GetX(ctx, 1001)
	require.False(t, row.LifecycleProjectionPending)
	require.Equal(t, revision, row.StateRevision, "delivery acknowledgment must not invalidate current operations")
}
func TestLifecycleLegacySeedIsBoundedAndSuppressed(t *testing.T) {
	client, repo := lifecycleSetup(t)
	defer repo.Close(t.Context())
	ctx := t.Context()
	row := client.User.Create().SetID(1001).SetUsername("mavey").SetEmail("mavey@example.com").SetIsActive(false).SaveX(ctx)
	start := time.Now().UTC()
	due, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "due"})
	require.NoError(t, err)
	require.Len(t, due.Seeded, 1)
	require.False(t, due.Seeded[0].State.LastEngagedAt.Before(start))
	require.False(t, due.Seeded[0].State.DesiredEnabled)
	seeded, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "seed_blocked", UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro(), AdmissionGeneration: client.User.GetX(ctx, row.ID).Lifecycle.AdmissionGeneration, Reason: "bot_banned"})
	require.NoError(t, err)
	require.True(t, seeded.State.IncidentAnnounced)
	require.True(t, seeded.State.DesiredEnabled, "legacy blocking cleared is_active without an owner decision")
	require.Equal(t, lifecycle.Blocked, seeded.State.RuntimeState)
	require.Equal(t, lifecycle.RestrictionBanned, seeded.State.ChatRestriction)
	_, err = repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "seed_ack", UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro()})
	require.NoError(t, err)
	due, err = repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "due"})
	require.NoError(t, err)
	require.Empty(t, due.Items)
	require.Empty(t, due.Seeded)
}
func TestLifecycleLegacyMissingGrantSeedsWithoutNewProviderEvent(t *testing.T) {
	client, repo := lifecycleSetup(t)
	defer repo.Close(t.Context())
	ctx := t.Context()
	row := client.User.Create().SetID(1002).SetUsername("legacy").SetEmail("legacy@example.com").SetIsActive(false).SaveX(ctx)
	_, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "due"})
	require.NoError(t, err)
	reply, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "seed_blocked", UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro(), AdmissionGeneration: client.User.GetX(ctx, row.ID).Lifecycle.AdmissionGeneration, Reason: "grant_missing"})
	require.NoError(t, err)
	require.True(t, reply.Applied)
	require.Equal(t, lifecycle.GrantMissing, reply.State.GrantHealth)
	require.NotEmpty(t, reply.State.IncidentID)
	require.True(t, reply.State.IncidentAnnounced, "migration must suppress another warning for the existing failure")
	require.True(t, reply.State.NoticeNextAttemptAt.IsZero())
	require.True(t, reply.State.GrantObservedAt.IsZero(), "migration must not fabricate provider evidence")
	require.False(t, reply.State.AllowsRuntime())
	_, err = repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "seed_ack", UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro()})
	require.NoError(t, err)
	due, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "due"})
	require.NoError(t, err)
	require.Empty(t, due.Seeded)
	require.Empty(t, due.Items)
}

func TestLifecycleRetractionQueueNeverOverwrites(t *testing.T) {
	now := time.Now().UTC()
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Recovering, AdmissionGeneration: 1, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy}
	for i := 0; i < 8; i++ {
		s.RuntimeState = lifecycle.Recovering
		s.OperationID = "op"
		s.IncidentID = lifecycleIdentity()
		changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "recovery_success", OperationID: "op", AdmissionGeneration: s.AdmissionGeneration}, now, false)
		require.NoError(t, err)
		require.True(t, changed)
	}
	require.Len(t, s.Retractions, 8)
	first := s.RetractionIncidentID
	s.RuntimeState = lifecycle.Recovering
	s.OperationID = "op"
	s.IncidentID = "ninth"
	changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "recovery_success", OperationID: "op", AdmissionGeneration: s.AdmissionGeneration}, now, false)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, first, s.RetractionIncidentID)
	require.Len(t, s.Retractions, 8)
	_, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "retraction_ack", IncidentID: first}, now, false)
	require.NoError(t, err)
	require.Len(t, s.Retractions, 7)
	require.Equal(t, s.Retractions[0].IncidentID, s.RetractionIncidentID)
}

func TestLifecycleCASRejectsRecreatedAccountWithSameRevision(t *testing.T) {
	client, repo := lifecycleSetup(t)
	defer repo.Close(t.Context())
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, 1001, "mavey", "Mavey", "mavey@example.com"))
	old := client.User.GetX(ctx, 1001)
	entered, resume := make(chan struct{}), make(chan struct{})
	var intercepted atomic.Bool
	client.User.Use(func(next entcore.Mutator) entcore.Mutator {
		return entcore.MutateFunc(func(ctx context.Context, m entcore.Mutation) (entcore.Value, error) {
			_, hasLifecycle := m.Field("lifecycle")
			if !hasLifecycle {
				return next.Mutate(ctx, m)
			}
			if !m.Op().Is(entcore.OpUpdate) {
				return next.Mutate(ctx, m)
			}
			if intercepted.CompareAndSwap(false, true) {
				close(entered)
				<-resume
			}
			return next.Mutate(ctx, m)
		})
	})
	result := make(chan usersrpc.LifecycleReply, 1)
	fail := make(chan error, 1)
	go func() {
		reply, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{UserID: old.ID, AccountCreatedAt: old.CreatedAt.UnixMicro(), Action: "ban", EventID: "old-ban", ObservedAt: time.Now()})
		result <- reply
		fail <- err
	}()
	<-entered
	client.User.DeleteOneID(old.ID).ExecX(ctx)
	replacement := client.User.Create().SetID(old.ID).SetUsername("replacement").SetEmail("replacement@example.com").SetCreatedAt(old.CreatedAt.Add(time.Second)).SaveX(ctx)
	require.Equal(t, old.StateRevision, replacement.StateRevision)
	close(resume)
	require.NoError(t, <-fail)
	require.False(t, (<-result).Applied)
	row := client.User.GetX(ctx, old.ID)
	require.True(t, row.IsActive)
	require.Nil(t, row.Lifecycle)
	require.Equal(t, "replacement", row.Username)
}
func TestLifecycleTimeoutFallbackCannotBecomePermanent(t *testing.T) {
	now := time.Now().UTC()
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, AdmissionGeneration: 1, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy, ChatSubscriptionID: "chat"}
	changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "timeout", EventID: "timeout", ObservedAt: now, TimeoutUntil: now.Add(time.Hour)}, now, false)
	require.NoError(t, err)
	require.True(t, changed)
	incident := s.IncidentID
	deadline := s.TimeoutUntil
	changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "ban", EventID: "fallback", ObservedAt: now.Add(time.Second), SubscriptionID: "chat"}, now, false)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, lifecycle.RestrictionTimeout, s.ChatRestriction)
	require.Equal(t, deadline, s.TimeoutUntil)
	require.Equal(t, incident, s.IncidentID)
}
func TestLifecycleGrantOrderingRejectsDelayedRestore(t *testing.T) {
	now := time.Now().UTC()
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, AdmissionGeneration: 1, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy}
	changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "grant_missing", EventID: "revoke", ObservedAt: now, AdmissionGeneration: s.AdmissionGeneration}, now, false)
	require.NoError(t, err)
	require.True(t, changed)
	generation := s.AdmissionGeneration
	changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "grant_restored", EventID: "old-grant", ObservedAt: now.Add(-time.Second), AdmissionGeneration: generation}, now, false)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, lifecycle.GrantMissing, s.GrantHealth)
	changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "grant_restored", EventID: "old-generation", ObservedAt: now.Add(time.Second), AdmissionGeneration: generation - 1}, now, false)
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "grant_restored", EventID: "fresh", ObservedAt: now.Add(time.Second), AdmissionGeneration: generation}, now, false)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, lifecycle.Recovering, s.RuntimeState)
}
func TestLifecycleEngagementCoalescesAtAuthorityAcrossReplicas(t *testing.T) {
	client, repo := lifecycleSetup(t)
	defer repo.Close(t.Context())
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, 1001, "mavey", "Mavey", "mavey@example.com"))
	row := client.User.GetX(ctx, 1001)
	at := row.Lifecycle.LastEngagedAt.Add(15 * time.Minute)
	request := usersrpc.LifecycleRequest{Action: "engage", UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro(), AdmissionGeneration: row.Lifecycle.AdmissionGeneration, ObservedAt: at, EventID: "viewer1"}
	first, err := repo.Lifecycle(ctx, request)
	require.NoError(t, err)
	require.True(t, first.Applied)
	request.EventID = "viewer2"
	request.ObservedAt = at.Add(time.Second)
	second, err := repo.Lifecycle(ctx, request)
	require.NoError(t, err)
	require.False(t, second.Applied)
	require.Equal(t, first.StateRevision, second.StateRevision)
	s := first.State
	s.RuntimeState = lifecycle.Dormant
	changed, err := transitionLifecycle(&s, request, at, false)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, lifecycle.Active, s.RuntimeState)
}
func TestLifecycleDormancyRequiresEvidenceAndClaimsProbeOnce(t *testing.T) {
	now := time.Now().UTC()
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, AdmissionGeneration: 1, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy, LastEngagedAt: now.Add(-lifecycle.DormancyPeriod)}
	r := usersrpc.LifecycleRequest{Action: "dormancy", AdmissionGeneration: 1}
	changed, err := transitionLifecycle(&s, r, now, false)
	require.NoError(t, err)
	require.False(t, changed)
	r.Action = "dormancy_claim"
	changed, err = transitionLifecycle(&s, r, now, false)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = transitionLifecycle(&s, r, now, false)
	require.NoError(t, err)
	require.False(t, changed)
	r.Action = "dormancy_probe"
	changed, err = transitionLifecycle(&s, r, now, false)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = transitionLifecycle(&s, r, now, false)
	require.NoError(t, err)
	require.False(t, changed)
	r.Action = "dormancy_live"
	_, err = transitionLifecycle(&s, r, now, false)
	require.NoError(t, err)
	require.Equal(t, now.Add(time.Hour), s.DormancyAfter)
	require.Equal(t, now.Add(-lifecycle.DormancyPeriod), s.LastEngagedAt)
	r.Action = "dormancy_defer"
	_, err = transitionLifecycle(&s, r, now, false)
	require.NoError(t, err)
	r.Action = "dormancy"
	r.Reason = "unavailable"
	changed, err = transitionLifecycle(&s, r, now.Add(23*time.Hour), false)
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = transitionLifecycle(&s, r, now.Add(24*time.Hour), false)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, lifecycle.Dormant, s.RuntimeState)
}
func TestNormalCleanupIsDurableBoundedAndCanceledByUnban(t *testing.T) {
	now := time.Now().UTC()
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, GrantHealth: lifecycle.GrantHealthy, ChatRestriction: lifecycle.RestrictionNone, AdmissionGeneration: 1}
	changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "ban", EventID: "ban", ObservedAt: now}, now, false)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, now, s.NormalCleanupAt)
	generation := s.AdmissionGeneration
	for attempt := 0; attempt < 3; attempt++ {
		at := s.NormalCleanupAt
		changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "cleanup_claim", AdmissionGeneration: generation}, at, false)
		require.NoError(t, err)
		require.True(t, changed)
		changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "cleanup_failed", AdmissionGeneration: generation}, at, false)
		require.NoError(t, err)
		require.True(t, changed)
		if attempt == 2 {
			require.Equal(t, at.Add(24*time.Hour), s.NormalCleanupAt)
		}
	}
	changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "unban", EventID: "unban", ObservedAt: now.Add(time.Second)}, now.Add(time.Second), false)
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, s.NormalCleanupAt.IsZero())
	changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "cleanup_success", AdmissionGeneration: generation}, now.Add(time.Second), false)
	require.NoError(t, err)
	require.False(t, changed)
}
func TestStaleLegacySeedCannotReplaceRestoredGrant(t *testing.T) {
	now := time.Now().UTC()
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, GrantHealth: lifecycle.GrantHealthy, ChatRestriction: lifecycle.RestrictionNone, AdmissionGeneration: 3, GrantObservedAt: now, GrantEventID: "restored"}
	changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "seed_blocked", AdmissionGeneration: 1, Reason: "grant_missing"}, now, false)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, lifecycle.GrantHealthy, s.GrantHealth)
	require.Empty(t, s.IncidentID)
}
func TestBannedGrantControlRepairRetriesBoundedly(t *testing.T) {
	now := time.Now().UTC()
	s := lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Blocked, ChatRestriction: lifecycle.RestrictionBanned, GrantHealth: lifecycle.GrantMissing, AdmissionGeneration: 2}
	changed, err := transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "grant_restored", EventID: "fresh-grant", ObservedAt: now, AdmissionGeneration: 2}, now, false)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, lifecycle.Blocked, s.RuntimeState)
	require.Equal(t, now, s.ControlRepairAt)
	generation := s.AdmissionGeneration
	for attempt := 0; attempt < 3; attempt++ {
		at := s.ControlRepairAt
		changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "control_claim", AdmissionGeneration: generation}, at, false)
		require.NoError(t, err)
		require.True(t, changed)
		changed, err = transitionLifecycle(&s, usersrpc.LifecycleRequest{Action: "control_failed", AdmissionGeneration: generation}, at, false)
		require.NoError(t, err)
		require.True(t, changed)
	}
	require.True(t, s.ControlRepairAt.IsZero())
	require.Equal(t, 3, s.ControlRepairAttempts)
	require.False(t, s.AllowsRuntime())
}
