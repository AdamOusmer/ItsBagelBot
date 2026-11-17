// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/lifecycleerasure"
	"ItsBagelBot/app/db/users/ent/premiumgrant"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/event/data"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type correctnessHarness struct {
	client *ent.Client
	pub    *bustest.Publisher
	repo   *repository.Users
}

func forUsersStorage(t *testing.T, scenario func(*testing.T, correctnessHarness)) {
	t.Helper()
	for _, backend := range []string{"sqlite", "mysql"} {
		t.Run(backend, func(t *testing.T) {
			scenario(t, newCorrectnessHarness(t, backend))
		})
	}
}

func newCorrectnessHarness(t *testing.T, backend string) correctnessHarness {
	t.Helper()
	if backend == "sqlite" {
		client, pub, repo := setup(t)
		return correctnessHarness{client, pub, repo}
	}
	raw := testdb.MySQL(t, "users_correctness_")
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", raw)))
	require.NoError(t, client.Schema.Create(t.Context()))
	pub := bustest.NewPublisher()
	repo := repository.NewUsers(repository.UsersConfig{Client: client, Packer: dbtest.NewPacker(t), Pub: pub, Log: zap.NewNop()})
	t.Cleanup(func() { repo.Close(context.Background()) })
	return correctnessHarness{client, pub, repo}
}

func (h correctnessHarness) registerGiftPair(t *testing.T) billingrpc.ApplyRequest {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, h.repo.Register(ctx, repository.Registration{ID: 7101, Username: "gifter", DisplayName: "Gifter", Email: "gifter@example.invalid"}))
	require.NoError(t, h.repo.Register(ctx, repository.Registration{ID: 7102, Username: "recipient", DisplayName: "Recipient", Email: "recipient@example.invalid"}))
	when := time.Now().UTC().Truncate(time.Second)
	expires := when.AddDate(0, 1, 0)
	return billingrpc.ApplyRequest{UserID: 7102, GifterID: 7101, EventID: "a-activate", Action: billingrpc.ActionActivate, OccurredAt: when, ExpiresAt: &expires}
}

type billingState struct {
	Status   user.Status
	Gifts    uint32
	Receipts int
}

func (h correctnessHarness) billingState(t *testing.T) billingState {
	t.Helper()
	ctx := t.Context()
	return billingState{h.client.User.GetX(ctx, 7102).Status, h.client.User.GetX(ctx, 7101).GiftsSent, h.client.BillingReceipt.Query().CountX(ctx)}
}

func TestBillingDurableReceiptSurvivesInterveningEqualTimeEvent(t *testing.T) {
	forUsersStorage(t, func(t *testing.T, h correctnessHarness) {
		gift := h.registerGiftPair(t)
		revoke := gift
		revoke.EventID, revoke.Action = "b-revoke", billingrpc.ActionRevoke
		steps := []struct {
			request billingrpc.ApplyRequest
			want    billingState
		}{
			{gift, billingState{user.StatusPaid, 1, 1}},
			{revoke, billingState{user.StatusFree, 1, 2}},
			{gift, billingState{user.StatusFree, 1, 2}},
			{revoke, billingState{user.StatusFree, 1, 2}},
		}
		for _, step := range steps {
			applied, err := h.repo.ApplyBilling(t.Context(), step.request)
			require.NoError(t, err)
			require.True(t, applied)
			assert.Equal(t, step.want, h.billingState(t))
		}
	})
}

func TestBillingSameSecondPlanSwitch(t *testing.T) {
	for _, startedFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("started_first=%t", startedFirst), func(t *testing.T) {
			forUsersStorage(t, func(t *testing.T, h correctnessHarness) {
				started := h.registerGiftPair(t)
				started.RecurringReference = "new-ref"
				require.NoError(t, h.client.User.UpdateOneID(started.UserID).SetStatus(user.StatusPaid).SetSubscriptionSource("tebex").SetSubscriptionRef("old-ref").Exec(t.Context()))
				ended := started
				ended.EventID, ended.Action, ended.RecurringReference = "z-ended", billingrpc.ActionRevoke, "old-ref"
				requests := []billingrpc.ApplyRequest{ended, started}
				if startedFirst {
					requests = []billingrpc.ApplyRequest{started, ended}
				}
				for _, req := range requests {
					_, err := h.repo.ApplyBilling(t.Context(), req)
					require.NoError(t, err)
				}
				row := h.client.User.GetX(t.Context(), started.UserID)
				assert.Equal(t, []any{user.StatusPaid, "new-ref", started.ExpiresAt, uint32(1)}, []any{row.Status, *row.SubscriptionRef, row.SubscriptionExpiresAt, h.client.User.GetX(t.Context(), started.GifterID).GiftsSent})
			})
		})
	}
}

func TestBillingMissingGifterStillActivatesRecipient(t *testing.T) {
	forUsersStorage(t, func(t *testing.T, h correctnessHarness) {
		gift := h.registerGiftPair(t)
		require.NoError(t, h.client.User.DeleteOneID(gift.GifterID).Exec(t.Context()))
		for range 2 {
			applied, err := h.repo.ApplyBilling(t.Context(), gift)
			require.NoError(t, err)
			assert.Equal(t, []any{true, user.StatusPaid, 1}, []any{applied, h.client.User.GetX(t.Context(), gift.UserID).Status, h.client.BillingReceipt.Query().CountX(t.Context())})
		}
	})
}

func TestBillingSkippedEventsCanBeResentAfterCorrection(t *testing.T) {
	for _, reason := range []string{"stale", "vip", "ignored"} {
		t.Run(reason, func(t *testing.T) {
			forUsersStorage(t, func(t *testing.T, h correctnessHarness) {
				req := h.registerGiftPair(t)
				q := h.client.User.UpdateOneID(req.UserID)
				switch reason {
				case "stale":
					q.SetBillingEventAt(req.OccurredAt.Add(time.Hour))
				case "vip":
					q.SetStatus(user.StatusVip)
				case "ignored":
					req.Action = billingrpc.ActionRevoke
				}
				require.NoError(t, q.Exec(t.Context()))
				applied, err := h.repo.ApplyBilling(t.Context(), req)
				require.NoError(t, err)
				assert.Equal(t, []any{false, 0}, []any{applied, h.client.BillingReceipt.Query().CountX(t.Context())})
				require.NoError(t, h.client.User.UpdateOneID(req.UserID).ClearBillingEventAt().SetStatus(user.StatusPaid).SetSubscriptionSource("tebex").Exec(t.Context()))
				applied, err = h.repo.ApplyBilling(t.Context(), req)
				require.NoError(t, err)
				receipt := h.client.BillingReceipt.Query().OnlyX(t.Context())
				assert.Equal(t, []bool{true, false}, []bool{applied, receipt.CreatedAt.IsZero()})
			})
		})
	}
}

func TestBillingGiftFailureRollsBackReceiptAndRetriesWholeTransaction(t *testing.T) {
	for _, failure := range []struct {
		name      string
		err       error
		retryable bool
	}{
		{"poison", errors.New("gift write failed"), false},
		{"deadlock", &mysql.MySQLError{Number: db.MySQLDeadlock}, true},
		{"lock_wait", &mysql.MySQLError{Number: db.MySQLLockWaitTimeout}, true},
	} {
		t.Run(failure.name, func(t *testing.T) {
			forUsersStorage(t, func(t *testing.T, h correctnessHarness) {
				gift := h.registerGiftPair(t)
				var writes atomic.Int32
				h.client.User.Use(failFirstGiftWrite(&writes, failure.err))
				applied, err := h.repo.ApplyBilling(t.Context(), gift)
				assert.Equal(t, failure.retryable, applied)
				if !failure.retryable {
					require.ErrorIs(t, err, failure.err)
					assert.Equal(t, billingState{user.StatusFree, 0, 0}, h.billingState(t))
					_, err = h.repo.ApplyBilling(t.Context(), gift)
				}
				require.NoError(t, err)
				assert.Equal(t, billingState{user.StatusPaid, 1, 1}, h.billingState(t))
				assert.Equal(t, int32(2), writes.Load())
			})
		})
	}
}

func failFirstGiftWrite(writes *atomic.Int32, failure error) ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			_, gift := m.AddedField(user.FieldGiftsSent)
			if gift && writes.Add(1) == 1 {
				return nil, failure
			}
			return next.Mutate(ctx, m)
		})
	}
}

func TestGrantProjectionPreservesConcurrentBillingOwnership(t *testing.T) {
	for _, source := range []string{"tebex", "admin"} {
		t.Run(source, func(t *testing.T) {
			forUsersStorage(t, func(t *testing.T, h correctnessHarness) {
				gift := h.registerGiftPair(t)
				now := gift.OccurredAt
				ctx := t.Context()
				require.NoError(t, h.client.User.UpdateOneID(gift.UserID).SetStatus(user.StatusPaid).SetSubscriptionSource("giveaway").Exec(ctx))
				h.client.PremiumGrant.Create().SetUserID(gift.UserID).SetGiveawayID("giveaway").SetAwardID("award").SetIntervalRuleVersion("v1").SetStartAt(now.Add(-2 * time.Hour)).SetEndAt(now.Add(-time.Hour)).SetState(premiumgrant.StateCommitted).SetProjectionPhase(premiumgrant.ProjectionPhaseActive).SaveX(ctx)
				var changed atomic.Bool
				h.client.User.Use(activateBeforeProjection(h, source, &changed))
				count, err := h.repo.ExpirePremiumGrants(ctx, now)
				require.NoError(t, err)
				assert.Equal(t, 1, count)
				row := h.client.User.GetX(ctx, gift.UserID)
				assert.Equal(t, []any{true, user.StatusPaid, source, "new-ref"}, []any{changed.Load(), row.Status, row.SubscriptionSource, *row.SubscriptionRef})
			})
		})
	}
}

func activateBeforeProjection(h correctnessHarness, source string, changed *atomic.Bool) ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			status, _ := m.Field(user.FieldStatus)
			if status != user.StatusFree || changed.Swap(true) {
				return next.Mutate(ctx, m)
			}
			err := h.client.User.UpdateOneID(7102).SetStatus(user.StatusPaid).SetSubscriptionSource(source).SetSubscriptionRef("new-ref").SetSubscriptionExpiresAt(time.Now().Add(time.Hour)).Exec(ctx)
			if err != nil {
				return nil, err
			}
			return next.Mutate(ctx, m)
		})
	}
}

func TestErasureRecoveryRepublishesOldRecordsAcrossChunks(t *testing.T) {
	forUsersStorage(t, func(t *testing.T, h correctnessHarness) {
		store := newFakeErasureStore()
		h.repo.SetErasureStore(store)
		when := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
		for id := range uint64(600) {
			store.objects[fmt.Sprintf("erasures/%d-111", id+8001)] = when
			h.client.LifecycleErasure.Create().SetUserID(id + 8001).SetAccountCreatedAt(111).SetDeletedAt(when).SaveX(t.Context())
		}
		committed := when.Add(time.Minute)
		h.client.LifecycleErasure.Update().Where(lifecycleerasure.UserIDEQ(8001)).SetDeletedAt(committed).ExecX(t.Context())
		var marked []int
		h.client.LifecycleErasure.Use(func(next ent.Mutator) ent.Mutator {
			return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				value, err := next.Mutate(ctx, m)
				require.NoError(t, err)
				marked = append(marked, value.(int))
				return value, err
			})
		})
		for sweep := range 2 {
			erased, err := h.repo.ReplayErasures(t.Context(), time.Now())
			require.NoError(t, err)
			assert.Zero(t, erased)
			events := h.pub.On(data.SubjectUserDeleted)
			require.Len(t, events, 600)
			if sweep == 0 {
				assertErasureTimes(t, events, committed, when)
			}
		}
		assert.Equal(t, []int{256, 256, 88}, marked)
	})
}

func assertErasureTimes(t *testing.T, events []*bus.Message, committed, recorded time.Time) {
	t.Helper()
	seen := make(map[uint64]bool)
	for i, event := range events {
		var deleted data.UserDeletedDTO
		require.NoError(t, codec.Unmarshal(event.Payload, &deleted))
		want := recorded
		if deleted.UserID == 8001 {
			want = committed
		}
		assert.True(t, deleted.DeletedAt.Equal(want))
		assert.Equal(t, uint64(8001+i), deleted.UserID)
		seen[deleted.UserID] = true
	}
	assert.Len(t, seen, 600)
}

func TestGrantSweepSkipsLegacyFreeBillingRows(t *testing.T) {
	forUsersStorage(t, func(t *testing.T, h correctnessHarness) {
		ctx := t.Context()
		now := time.Now().UTC()
		for i, source := range []string{"tebex", "admin", ""} {
			id := uint64(7201 + i)
			h.client.User.Create().SetID(id).SetUsername(fmt.Sprint(id)).SetEmail(fmt.Sprintf("%d@example.invalid", id)).SetSubscriptionSource(source).SaveX(ctx)
			h.client.PremiumGrant.Create().SetUserID(id).SetGiveawayID("legacy").SetAwardID(fmt.Sprint(id)).SetIntervalRuleVersion("v1").SetStartAt(now.Add(-time.Hour)).SetEndAt(now.Add(time.Hour)).SetState(premiumgrant.StateCommitted).SetProjectionPhase(premiumgrant.ProjectionPhaseActive).SaveX(ctx)
		}
		for range 2 {
			count, err := h.repo.ExpirePremiumGrants(ctx, now)
			require.NoError(t, err)
			assert.Zero(t, count)
		}
		assert.Equal(t, []user.Status{user.StatusFree, user.StatusFree, user.StatusPaid}, []user.Status{h.client.User.GetX(ctx, 7201).Status, h.client.User.GetX(ctx, 7202).Status, h.client.User.GetX(ctx, 7203).Status})
		assert.Len(t, h.pub.On(data.SubjectUserChanged), 1)
	})
}

type erasureFailurePublisher struct {
	*bustest.Publisher
	client   *ent.Client
	failures map[uint64]error
	attempts []uint64
	tooEarly bool
}

func (p *erasureFailurePublisher) PublishOwned(ctx context.Context, subject string, payload []byte) error {
	if subject != data.SubjectUserDeleted {
		return p.Publisher.PublishOwned(ctx, subject, payload)
	}
	var dto data.UserDeletedDTO
	if err := codec.Unmarshal(payload, &dto); err != nil {
		return err
	}
	p.attempts = append(p.attempts, dto.UserID)
	if dto.UserID < 9001 {
		p.tooEarly = p.tooEarly || p.client.User.Query().ExistX(ctx)
	}
	if err := p.failures[dto.UserID]; err != nil {
		return err
	}
	return p.Publisher.PublishOwned(ctx, subject, payload)
}

func TestErasureRecoveryRestoresAllChunksBeforeRepublishAndJoinsFailures(t *testing.T) {
	forUsersStorage(t, func(t *testing.T, h correctnessHarness) {
		ctx := t.Context()
		first, second := errors.New("first publish failed"), errors.New("second publish failed")
		pub := &erasureFailurePublisher{Publisher: h.pub, client: h.client, failures: map[uint64]error{8001: first, 8257: second}}
		repo := repository.NewUsers(repository.UsersConfig{Client: h.client, Packer: dbtest.NewPacker(t), Pub: pub, Log: zap.NewNop()})
		t.Cleanup(func() { repo.Close(context.Background()) })
		store := seedErasureRecovery(t, h)
		repo.SetErasureStore(store)
		erased, err := repo.ReplayErasures(ctx, time.Now())
		require.ErrorIs(t, err, first)
		require.ErrorIs(t, err, second)
		assert.Equal(t, []any{2, false, 0, 602, 600}, []any{erased, pub.tooEarly, h.client.User.Query().CountX(ctx), len(pub.attempts), len(h.pub.On(data.SubjectUserDeleted))})
		endpoints := append(pub.attempts[:3:3], pub.attempts[len(pub.attempts)-1])
		assert.Equal(t, []uint64{9001, 9002, 8001, 8600}, endpoints)
	})
}

func seedErasureRecovery(t *testing.T, h correctnessHarness) *fakeErasureStore {
	t.Helper()
	store := newFakeErasureStore()
	when := time.Now().UTC().Add(-30 * 24 * time.Hour)
	for id := uint64(8001); id <= 8600; id++ {
		store.objects[fmt.Sprintf("erasures/%d-111", id)] = when
		h.client.LifecycleErasure.Create().SetUserID(id).SetAccountCreatedAt(111).SetDeletedAt(when).SaveX(t.Context())
	}
	for _, id := range []uint64{9001, 9002} {
		created := when.Truncate(time.Second)
		h.client.User.Create().SetID(id).SetUsername(fmt.Sprint(id)).SetEmail(fmt.Sprintf("%d@example.invalid", id)).SetCreatedAt(created).SaveX(t.Context())
		store.objects[fmt.Sprintf("erasures/%d-%d", id, created.UnixMicro())] = when
	}
	return store
}
