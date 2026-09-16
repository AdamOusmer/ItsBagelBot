// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/adminuser"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/ent/premiumgrant"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/event/data"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestGiveawayPoolUsesAuthoritativeRulesAndPendingFlags(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	for id, name := range map[uint64]string{1: "eligible", 2: "inactive", 3: "staff", 4: "former", 5: "test", 6: "vip", 7: "banned", 8: "onboarding"} {
		require.NoError(t, repo.Register(ctx, id, name, name, name+"@example.com"))
	}
	require.NoError(t, repo.SetOnboarded(ctx, 1, true)) // still pending in the batcher
	require.NoError(t, repo.SetOnboarded(ctx, 2, true))
	require.NoError(t, repo.SetActive(ctx, 2, false)) // pending inactive must be visible
	require.NoError(t, repo.SetOnboarded(ctx, 3, true))
	require.NoError(t, repo.SetOnboarded(ctx, 4, true))
	require.NoError(t, repo.SetOnboarded(ctx, 5, true))
	require.NoError(t, repo.SetOnboarded(ctx, 6, true))
	require.NoError(t, repo.SetOnboarded(ctx, 7, true))
	require.NoError(t, repo.SetOnboarded(ctx, 8, false))
	require.NoError(t, client.User.UpdateOneID(3).SetStatus(user.StatusFree).Exec(ctx))
	require.NoError(t, client.User.UpdateOneID(4).SetStatus(user.StatusFree).Exec(ctx))
	require.NoError(t, client.User.UpdateOneID(5).SetTestAccount(true).Exec(ctx))
	require.NoError(t, client.User.UpdateOneID(6).SetStatus(user.StatusVip).Exec(ctx))
	require.NoError(t, repo.SetBanned(ctx, 7, true))
	// Only active roster membership excludes a staff account. A former staff
	// row remains eligible after it is disabled.
	_, err := client.AdminUser.Create().SetID(3).SetLogin("staff").SetDisplayName("Staff").SetRole(adminuser.RoleAdmin).Save(ctx)
	require.NoError(t, err)
	_, err = client.AdminUser.Create().SetID(4).SetLogin("former").SetDisplayName("Former").SetRole(adminuser.RoleAdmin).SetActive(false).Save(ctx)
	require.NoError(t, err)

	p, err := repo.GiveawayPoolSnapshot(ctx, nil)
	require.NoError(t, err)
	require.Len(t, p.Candidates, 2)
	assert.Equal(t, []uint64{1, 4}, []uint64{p.Candidates[0].UserID, p.Candidates[1].UserID})
	assert.Equal(t, repository.GiveawayPoolCounts{Total: 8, Eligible: 2, Inactive: 1, NotOnboarded: 1, TestAccount: 1, VIP: 1, Banned: 1, CurrentStaff: 1}, p.Counts)
}

func TestSetTestAccountRequiresAdminAndAudits(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 10, "target", "target", "target@example.com"))
	_, err := client.AdminUser.Create().SetID(11).SetLogin("mod").SetDisplayName("Mod").SetRole(adminuser.RoleModerator).Save(ctx)
	require.NoError(t, err)
	assert.Error(t, repo.SetTestAccount(ctx, 10, true, 11))
	_, err = client.AdminUser.Create().SetID(12).SetLogin("admin").SetDisplayName("Admin").SetRole(adminuser.RoleAdmin).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.SetTestAccount(ctx, 10, true, 12))
	assert.True(t, client.User.GetX(ctx, 10).TestAccount)
	audit, err := client.AdminAudit.Query().Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, "set_test_account", audit.Action)
}

func TestPremiumGrantPreparationIsIdempotentAndConcurrent(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 20, "winner", "winner", "winner@example.com"))
	req := usersrpc.PreparePremiumGrantRequest{GiveawayID: "g-1", AwardID: "a-1", UserID: 20,
		StartAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), EndAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), IntervalRuleVersion: "tebex-monthly-v1"}
	rows := concurrentPrepares(t, ctx, repo, req)
	var first usersrpc.PremiumGrant
	for _, row := range rows {
		if first.ID == 0 {
			first = row
		}
		assert.Equal(t, first.ID, row.ID)
	}
	assert.Equal(t, 1, first.ID)

	committed, err := repo.CommitPremiumGrant(ctx, usersrpc.CommitPremiumGrantRequest{GiveawayID: "g-1", AwardID: "a-1", UserID: 20})
	require.NoError(t, err)
	assert.Equal(t, "committed", committed.State)
	again, err := repo.CommitPremiumGrant(ctx, usersrpc.CommitPremiumGrantRequest{GiveawayID: "g-1", AwardID: "a-1", UserID: 20})
	require.NoError(t, err)
	assert.Equal(t, committed.ID, again.ID)
}

func concurrentPrepares(t *testing.T, ctx context.Context, repo *repository.Users, req usersrpc.PreparePremiumGrantRequest) []usersrpc.PremiumGrant {
	t.Helper()
	const n = 6
	rows := make(chan usersrpc.PremiumGrant, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			row, err := prepareWithSQLiteRetry(ctx, repo, req)
			if err != nil {
				errs <- err
				return
			}
			rows <- row
		}()
	}
	wg.Wait()
	close(rows)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	result := make([]usersrpc.PremiumGrant, 0, n)
	for row := range rows {
		result = append(result, row)
	}
	return result
}

func prepareWithSQLiteRetry(ctx context.Context, repo *repository.Users, req usersrpc.PreparePremiumGrantRequest) (usersrpc.PremiumGrant, error) {
	var row usersrpc.PremiumGrant
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		row, err = repo.PreparePremiumGrant(ctx, req)
		if err == nil || !strings.Contains(err.Error(), "locked") {
			return row, err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return row, err
}

func TestPremiumGrantCancellationExpiryAndCoverageStack(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 30, "stacked", "stacked", "stacked@example.com"))
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	makeGrant := func(award string, from, to time.Time) usersrpc.PremiumGrant {
		g, err := repo.PreparePremiumGrant(ctx, usersrpc.PreparePremiumGrantRequest{GiveawayID: "g-2", AwardID: award, UserID: 30, StartAt: from, EndAt: to, IntervalRuleVersion: "tebex-monthly-v1"})
		require.NoError(t, err)
		g, err = repo.CommitPremiumGrant(ctx, usersrpc.CommitPremiumGrantRequest{GiveawayID: "g-2", AwardID: award, UserID: 30})
		require.NoError(t, err)
		return g
	}
	makeGrant("a-1", start, start.AddDate(0, 1, 0))
	makeGrant("a-2", start.AddDate(0, 1, 0), start.AddDate(0, 2, 0))
	prepared, err := repo.PreparePremiumGrant(ctx, usersrpc.PreparePremiumGrantRequest{GiveawayID: "g-2", AwardID: "a-cancel", UserID: 30, StartAt: start.AddDate(0, 2, 0), EndAt: start.AddDate(0, 2, 0).Add(time.Hour), IntervalRuleVersion: "tebex-monthly-v1"})
	require.NoError(t, err)
	require.NoError(t, repo.CancelPremiumGrant(ctx, usersrpc.CommitPremiumGrantRequest{GiveawayID: prepared.GiveawayID, AwardID: prepared.AwardID, UserID: prepared.UserID}))
	_, err = repo.CommitPremiumGrant(ctx, usersrpc.CommitPremiumGrantRequest{GiveawayID: prepared.GiveawayID, AwardID: prepared.AwardID, UserID: prepared.UserID})
	assert.Error(t, err)

	coverage, err := repo.PremiumCoverage(ctx, 30, start.Add(24*time.Hour))
	require.NoError(t, err)
	assert.Len(t, coverage.Grants, 2)
	count, err := repo.ExpirePremiumGrants(ctx, start.AddDate(0, 2, 0))
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	coverage, err = repo.PremiumCoverage(ctx, 30, start.AddDate(0, 2, 0))
	require.NoError(t, err)
	assert.Empty(t, coverage.Grants)
}

func TestPremiumGrantBoundarySweepAdvancesProjectionPhase(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 31, "boundary", "boundary", "boundary@example.com"))
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	_, err := repo.PreparePremiumGrant(ctx, usersrpc.PreparePremiumGrantRequest{GiveawayID: "g-boundary", AwardID: "a-boundary", UserID: 31, StartAt: start, EndAt: end, IntervalRuleVersion: "tebex-monthly-v1"})
	require.NoError(t, err)
	_, err = repo.CommitPremiumGrant(ctx, usersrpc.CommitPremiumGrantRequest{GiveawayID: "g-boundary", AwardID: "a-boundary", UserID: 31})
	require.NoError(t, err)
	assert.Equal(t, user.StatusFree, client.User.GetX(ctx, 31).Status, "future interval is not paid until it covers now")
	count, err := repo.ExpirePremiumGrants(ctx, start)
	require.NoError(t, err)
	assert.Zero(t, count)
	grant := client.PremiumGrant.Query().OnlyX(ctx)
	assert.Equal(t, "active", string(grant.ProjectionPhase))
	assert.Equal(t, user.StatusPaid, client.User.GetX(ctx, 31).Status)
	assert.Equal(t, "giveaway", client.User.GetX(ctx, 31).SubscriptionSource)
	assert.NotEmpty(t, pub.On(data.SubjectUserChanged))
	count, err = repo.ExpirePremiumGrants(ctx, end)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	grant = client.PremiumGrant.Query().OnlyX(ctx)
	assert.Equal(t, "expired", string(grant.ProjectionPhase))
	assert.Equal(t, user.StatusFree, client.User.GetX(ctx, 31).Status)
	assert.Equal(t, "", client.User.GetX(ctx, 31).SubscriptionSource)
}

// rejectInvalidateStatus is the production JetStream catalog: bagel.cache.invalidate.status
// has no stream. The sweep used to PublishJSON through this publisher and stall
// projection_phase at pending. Core NATS (nc) is the invalidate path now; with
// prefix set and nc nil the sweep must still advance.
type rejectInvalidateStatus struct {
	inner *bustest.Publisher
}

func (p *rejectInvalidateStatus) PublishOwned(ctx context.Context, subject string, payload []byte) error {
	if strings.Contains(subject, "cache.invalidate") && strings.HasSuffix(subject, ".status") {
		return errors.New(`bus: no stream matches subject "bagel.cache.invalidate.status"`)
	}
	return p.inner.PublishOwned(ctx, subject, payload)
}

func (p *rejectInvalidateStatus) PublishOwnedWithID(ctx context.Context, subject, _ string, payload []byte) error {
	return p.PublishOwned(ctx, subject, payload)
}

func (p *rejectInvalidateStatus) Flush(ctx context.Context) error { return p.inner.Flush(ctx) }
func (p *rejectInvalidateStatus) Close() error                    { return p.inner.Close() }

func TestPremiumGrantBoundarySweepIgnoresJetStreamStatusInvalidation(t *testing.T) {
	client := testdb.Open(t, "grantinvalidate", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	inner := bustest.NewPublisher()
	repo := repository.NewUsers(client, newPacker(t), &rejectInvalidateStatus{inner: inner}, nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	repo.SetInvalidationPrefix("bagel.cache.invalidate")

	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 33, "jsboundary", "jsboundary", "jsboundary@example.com"))
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	_, err := repo.PreparePremiumGrant(ctx, usersrpc.PreparePremiumGrantRequest{
		GiveawayID: "g-js", AwardID: "a-js", UserID: 33, StartAt: start, EndAt: end, IntervalRuleVersion: "tebex-monthly-v1",
	})
	require.NoError(t, err)
	_, err = repo.CommitPremiumGrant(ctx, usersrpc.CommitPremiumGrantRequest{GiveawayID: "g-js", AwardID: "a-js", UserID: 33})
	require.NoError(t, err)

	count, err := repo.ExpirePremiumGrants(ctx, start)
	require.NoError(t, err)
	assert.Zero(t, count)
	grant := client.PremiumGrant.Query().OnlyX(ctx)
	assert.Equal(t, "active", string(grant.ProjectionPhase))
	assert.NotEmpty(t, inner.On(data.SubjectUserChanged))
	assert.Empty(t, inner.On("bagel.cache.invalidate.status"))

	count, err = repo.ExpirePremiumGrants(ctx, end)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	grant = client.PremiumGrant.Query().OnlyX(ctx)
	assert.Equal(t, "expired", string(grant.ProjectionPhase))
}

func TestPremiumGrantNormalizesMicrosecondsForReplay(t *testing.T) {
	_, _, repo := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 32, "precision", "precision", "precision@example.com"))
	start := time.Date(2026, 10, 1, 0, 0, 0, 123456789, time.FixedZone("offset", -4*60*60))
	end := start.Add(31 * 24 * time.Hour).Add(987 * time.Nanosecond)
	req := usersrpc.PreparePremiumGrantRequest{GiveawayID: "g-precision", AwardID: "a-precision", UserID: 32, StartAt: start, EndAt: end, IntervalRuleVersion: "verified-tebex-monthly"}
	first, err := repo.PreparePremiumGrant(ctx, req)
	require.NoError(t, err)
	replay, err := repo.PreparePremiumGrant(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, first.ID, replay.ID)
	assert.Equal(t, start.UTC().Truncate(time.Microsecond), first.StartAt)
	assert.Equal(t, end.UTC().Truncate(time.Microsecond), first.EndAt)
}

func TestCommittedGrantOverlaysBillingAndRevokeKeepsPrize(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.Register(ctx, 40, "subscriber", "subscriber", "subscriber@example.com"))
	now := time.Now().UTC().Truncate(time.Second)
	paidUntil := now.Add(time.Hour)
	_, err := repo.ApplyBilling(ctx, billingrpc.ApplyRequest{UserID: 40, EventID: "paid-1", Action: billingrpc.ActionActivate,
		OccurredAt: now, ExpiresAt: &paidUntil, RecurringReference: "tbx-r-40"})
	require.NoError(t, err)
	grant, err := repo.PreparePremiumGrant(ctx, usersrpc.PreparePremiumGrantRequest{GiveawayID: "g-billing", AwardID: "a-40", UserID: 40,
		StartAt: now.Add(-time.Minute), EndAt: now.Add(2 * time.Hour), IntervalRuleVersion: "verified-tebex-monthly"})
	require.NoError(t, err)
	_, err = repo.CommitPremiumGrant(ctx, usersrpc.CommitPremiumGrantRequest{GiveawayID: grant.GiveawayID, AwardID: grant.AwardID, UserID: grant.UserID})
	require.NoError(t, err)
	view, err := repo.Get(ctx, 40)
	require.NoError(t, err)
	assert.Equal(t, "paid", view.Status)

	// A source-specific termination clears only the Tebex agreement. The
	// committed prize remains effective and its interval is still readable.
	_, err = repo.ApplyBilling(ctx, billingrpc.ApplyRequest{UserID: 40, EventID: "paid-revoke", Action: billingrpc.ActionRevoke,
		OccurredAt: now.Add(time.Minute), RecurringReference: "tbx-r-40"})
	require.NoError(t, err)
	stored := client.User.GetX(ctx, 40)
	assert.Equal(t, user.StatusPaid, stored.Status)
	assert.Equal(t, "giveaway", stored.SubscriptionSource)
	view, err = repo.Get(ctx, 40)
	require.NoError(t, err)
	assert.Equal(t, "paid", view.Status)
	coverage, err := repo.PremiumCoverage(ctx, 40, now)
	require.NoError(t, err)
	assert.Nil(t, coverage.RecurringReference)
	assert.Len(t, coverage.Grants, 1)
	assert.Nil(t, stored.SubscriptionRef)

	// Ending the grant restores the source row's stored free state.
	require.NoError(t, client.PremiumGrant.UpdateOneID(grant.ID).SetEndAt(now.Add(-time.Second)).Exec(ctx))
	count, err := repo.ExpirePremiumGrants(ctx, now)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	stored = client.User.GetX(ctx, 40)
	assert.Equal(t, user.StatusFree, stored.Status)
	assert.Equal(t, "", stored.SubscriptionSource)
	view, err = repo.Get(ctx, 40)
	require.NoError(t, err)
	assert.Equal(t, "free", view.Status)
}

func TestGrantSweepWritesPaidForAlreadyActiveCoverage(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	now := time.Now().UTC()
	require.NoError(t, repo.Register(ctx, 34, "already", "already", "already@example.com"))
	client.PremiumGrant.Create().
		SetUserID(34).
		SetGiveawayID("g-already").
		SetAwardID("a-already").
		SetState(premiumgrant.StateCommitted).
		SetProjectionPhase(premiumgrant.ProjectionPhaseActive).
		SetStartAt(now.Add(-time.Hour)).
		SetEndAt(now.AddDate(0, 1, 0)).
		SetIntervalRuleVersion("promotional-calendar-month-v1").
		ExecX(ctx)
	assert.Equal(t, user.StatusFree, client.User.GetX(ctx, 34).Status)

	count, err := repo.ExpirePremiumGrants(ctx, now)
	require.NoError(t, err)
	assert.Zero(t, count)
	row := client.User.GetX(ctx, 34)
	assert.Equal(t, user.StatusPaid, row.Status)
	assert.Equal(t, "giveaway", row.SubscriptionSource)
	_, _, paid, _, err := repo.UserStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, paid)
}

func TestCommittedGrantDoesNotDemoteVIP(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	now := time.Now().UTC()
	require.NoError(t, repo.Register(ctx, 35, "vipgrant", "vipgrant", "vipgrant@example.com"))
	require.NoError(t, repo.SetStatus(ctx, 35, user.StatusVip))
	_, err := repo.PreparePremiumGrant(ctx, usersrpc.PreparePremiumGrantRequest{
		GiveawayID: "g-vip", AwardID: "a-vip", UserID: 35, StartAt: now.Add(-time.Minute), EndAt: now.AddDate(0, 1, 0), IntervalRuleVersion: "tebex-monthly-v1",
	})
	require.NoError(t, err)
	_, err = repo.CommitPremiumGrant(ctx, usersrpc.CommitPremiumGrantRequest{GiveawayID: "g-vip", AwardID: "a-vip", UserID: 35})
	require.NoError(t, err)
	assert.Equal(t, user.StatusVip, client.User.GetX(ctx, 35).Status)
}

func TestExpireSubscriptionsKeepsCoveringGrantPaid(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, repo.Register(ctx, 36, "gracegrant", "gracegrant", "gracegrant@example.com"))
	expiry := now.Add(-time.Minute)
	_, err := repo.ApplyBilling(ctx, billingrpc.ApplyRequest{
		UserID: 36, EventID: "evt-g", Action: billingrpc.ActionActivate,
		OccurredAt: now.Add(-48 * time.Hour), ExpiresAt: &expiry, RecurringReference: "tbx-r-36",
	})
	require.NoError(t, err)
	_, err = repo.PreparePremiumGrant(ctx, usersrpc.PreparePremiumGrantRequest{
		GiveawayID: "g-grace", AwardID: "a-grace", UserID: 36, StartAt: now.Add(-time.Hour), EndAt: now.AddDate(0, 1, 0), IntervalRuleVersion: "tebex-monthly-v1",
	})
	require.NoError(t, err)
	_, err = repo.CommitPremiumGrant(ctx, usersrpc.CommitPremiumGrantRequest{GiveawayID: "g-grace", AwardID: "a-grace", UserID: 36})
	require.NoError(t, err)

	count, err := repo.ExpireSubscriptions(ctx, now, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	row := client.User.GetX(ctx, 36)
	assert.Equal(t, user.StatusPaid, row.Status)
	assert.Equal(t, "giveaway", row.SubscriptionSource)
	assert.Nil(t, row.SubscriptionRef)
}
