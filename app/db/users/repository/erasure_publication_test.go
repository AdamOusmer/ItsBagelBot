// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/lifecycleerasure"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/event/data"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/db"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestErasurePublicationSurvivesLifecycleCompletionAndPruning(t *testing.T) {
	forUsersStorage(t, func(t *testing.T, h correctnessHarness) {
		ctx := t.Context()
		failure := errors.New("deletion publication lost")
		pub := &erasureFailurePublisher{Publisher: h.pub, client: h.client, failures: map[uint64]error{1001: failure}}
		repo := repository.NewUsers(repository.UsersConfig{Client: h.client, Packer: dbtest.NewPacker(t), Pub: pub, Log: zap.NewNop()})
		t.Cleanup(func() { repo.Close(context.Background()) })
		store := newFakeErasureStore()
		repo.SetErasureStore(store)
		require.NoError(t, repo.Register(ctx, repository.Registration{ID: 1001, Username: "mavey", DisplayName: "Mavey", Email: "mavey@example.invalid"}))
		require.ErrorIs(t, repo.Delete(ctx, 1001), failure)
		marker := h.client.LifecycleErasure.Query().OnlyX(ctx)
		assert.Equal(t, []any{0, 0, true}, []any{h.client.User.Query().CountX(ctx), len(pub.On(data.SubjectUserDeleted)), marker.DeletedPublishedAt == nil})
		for _, participant := range []string{"notifications", "outgress"} {
			_, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "erase_ack", UserID: 1001, AccountCreatedAt: marker.AccountCreatedAt, Reason: participant})
			require.NoError(t, err)
		}
		require.NotNil(t, h.client.LifecycleErasure.GetX(ctx, marker.ID).CompletedAt)
		old := time.Now().UTC().Add(-10 * 24 * time.Hour)
		h.client.LifecycleErasure.UpdateOneID(marker.ID).SetCompletedAt(old).ExecX(ctx)
		for key := range store.objects {
			store.objects[key] = old
		}
		_, err := repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "due"})
		require.NoError(t, err)
		assert.Equal(t, 1, h.client.LifecycleErasure.Query().CountX(ctx))
		_, err = repo.ReplayErasures(ctx, time.Now())
		require.ErrorIs(t, err, failure)
		assert.Nil(t, h.client.LifecycleErasure.GetX(ctx, marker.ID).DeletedPublishedAt)
		delete(pub.failures, 1001)
		for range 2 {
			_, err = repo.ReplayErasures(ctx, time.Now())
			require.NoError(t, err)
			assert.Equal(t, []any{1, 1}, []any{len(pub.On(data.SubjectUserDeleted)), h.client.LifecycleErasure.Query().Where(lifecycleerasure.DeletedPublishedAtNotNil()).CountX(ctx)})
		}
		var resent data.UserDeletedDTO
		require.NoError(t, codec.Unmarshal(pub.On(data.SubjectUserDeleted)[0].Payload, &resent))
		assert.Equal(t, []any{uint64(1001), marker.AccountCreatedAt, true}, []any{resent.UserID, resent.AccountCreatedAt, resent.DeletedAt.Equal(marker.DeletedAt)})
		_, err = repo.Lifecycle(ctx, usersrpc.LifecycleRequest{Action: "due"})
		require.NoError(t, err)
		assert.Zero(t, h.client.LifecycleErasure.Query().CountX(ctx))
	})
}

type markerFailure struct {
	name   string
	err    error
	once   bool
	marked int
}

func (f markerFailure) hook(poison int, calls *atomic.Int32, healed *atomic.Bool) ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			ids, err := m.(*ent.LifecycleErasureMutation).IDs(ctx)
			if err != nil {
				return nil, err
			}
			if !slices.Contains(ids, poison) || healed.Load() {
				return next.Mutate(ctx, m)
			}
			if calls.Add(1) == 1 || !f.once {
				return nil, f.err
			}
			return next.Mutate(ctx, m)
		})
	}
}

func TestErasurePublicationMarkerFailuresAreIsolatedAndRetried(t *testing.T) {
	for _, failure := range []markerFailure{
		{"poison", errors.New("marker write failed"), false, 2},
		{"deadlock", &mysql.MySQLError{Number: db.MySQLDeadlock}, true, 3},
		{"lock_wait", &mysql.MySQLError{Number: db.MySQLLockWaitTimeout}, true, 3},
	} {
		t.Run(failure.name, func(t *testing.T) {
			forUsersStorage(t, func(t *testing.T, h correctnessHarness) {
				ctx := t.Context()
				store := newFakeErasureStore()
				h.repo.SetErasureStore(store)
				for id := uint64(1001); id <= 1003; id++ {
					store.objects[fmt.Sprintf("erasures/%d-111", id)] = time.Now().Add(-time.Hour)
					h.client.LifecycleErasure.Create().SetUserID(id).SetAccountCreatedAt(111).SaveX(ctx)
				}
				poison := h.client.LifecycleErasure.Query().Where(lifecycleerasure.UserIDEQ(1002)).OnlyX(ctx).ID
				var calls atomic.Int32
				var healed atomic.Bool
				h.client.LifecycleErasure.Use(failure.hook(poison, &calls, &healed))
				_, err := h.repo.ReplayErasures(ctx, time.Now())
				if failure.once {
					require.NoError(t, err)
					assert.Equal(t, int32(2), calls.Load())
				} else {
					require.ErrorIs(t, err, failure.err)
				}
				assert.Equal(t, []int{3, failure.marked}, []int{len(h.pub.On(data.SubjectUserDeleted)), h.client.LifecycleErasure.Query().Where(lifecycleerasure.DeletedPublishedAtNotNil()).CountX(ctx)})
				healed.Store(true)
				_, err = h.repo.ReplayErasures(ctx, time.Now())
				require.NoError(t, err)
				assert.Equal(t, []int{6 - failure.marked, 3}, []int{len(h.pub.On(data.SubjectUserDeleted)), h.client.LifecycleErasure.Query().Where(lifecycleerasure.DeletedPublishedAtNotNil()).CountX(ctx)})
			})
		})
	}
}
