// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/tokens"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/objstore"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeErasureStore struct {
	mu      sync.Mutex
	objects map[string]time.Time
	putErr  error
}

func newFakeErasureStore() *fakeErasureStore {
	return &fakeErasureStore{objects: map[string]time.Time{}}
}

func (s *fakeErasureStore) PutNew(_ context.Context, key string, _ []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.putErr != nil {
		return s.putErr
	}
	if _, ok := s.objects[key]; ok {
		return objstore.ErrExists
	}
	s.objects[key] = time.Now()
	return nil
}

func (s *fakeErasureStore) List(_ context.Context, prefix string) ([]objstore.Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []objstore.Object
	for key, at := range s.objects {
		if strings.HasPrefix(key, prefix) {
			out = append(out, objstore.Object{Key: key, LastModified: at})
		}
	}
	return out, nil
}

func (s *fakeErasureStore) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.objects))
	for key := range s.objects {
		out = append(out, key)
	}
	return out
}

type fakeRevoker struct{ revoked []string }

func (f *fakeRevoker) Revoke(_ context.Context, token string) error {
	f.revoked = append(f.revoked, token)
	return nil
}

func TestDeleteRecordsErasureOutsideDatabaseAndRevokesTwitchTokens(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()
	store, revoker := newFakeErasureStore(), &fakeRevoker{}
	repo.SetErasureStore(store)
	repo.SetTokenRevoker(revoker)

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "Mavey", "mavey@example.com"))
	require.NoError(t, repo.UpsertToken(ctx, 1001, tokens.TypeAccessToken, tokens.PlatformTwitch, []byte("broadcaster-tok"), []byte("refresh"), nil))
	require.NoError(t, repo.UpsertToken(ctx, 1001, tokens.TypeUserToken, tokens.PlatformTwitch, []byte("user-tok"), nil, nil))
	createdAt := client.User.Query().Where(user.IDEQ(1001)).OnlyX(ctx).CreatedAt.UnixMicro()

	require.NoError(t, repo.Delete(ctx, 1001))

	wantKeys := []string{fmt.Sprintf("erasures/1001-%d", createdAt)}
	assert.Equal(t, wantKeys, store.keys())
	assert.ElementsMatch(t, []string{"broadcaster-tok", "user-tok"}, revoker.revoked)
	assert.Equal(t, [2]int{0, 1}, [2]int{client.User.Query().CountX(ctx), len(pub.On(data.SubjectUserDeleted))})
}

func TestDeleteKeepsAccountWhenErasureRecordFails(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()
	store := newFakeErasureStore()
	store.putErr = errors.New("r2 unavailable")
	repo.SetErasureStore(store)

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "Mavey", "mavey@example.com"))

	require.Error(t, repo.Delete(ctx, 1001))
	assert.Equal(t, 1, client.User.Query().CountX(ctx), "no record outside the database, no deletion")
	assert.Empty(t, pub.On(data.SubjectUserDeleted))

	store.putErr = nil
	require.NoError(t, repo.Delete(ctx, 1001))
	assert.Equal(t, 0, client.User.Query().CountX(ctx))
}

func TestReplayErasuresDeletesOnlyRestoredIncarnations(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()
	store := newFakeErasureStore()
	repo.SetErasureStore(store)

	restored := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	client.User.Create().SetID(1001).SetUsername("restored").SetEmail("a@example.com").SetCreatedAt(restored).SaveX(ctx)
	client.User.Create().SetID(1002).SetUsername("reregistered").SetEmail("b@example.com").SetCreatedAt(restored.Add(time.Hour)).SaveX(ctx)
	client.User.Create().SetID(1003).SetUsername("untouched").SetEmail("c@example.com").SetCreatedAt(restored).SaveX(ctx)
	long := time.Now().Add(-2 * time.Hour)
	store.objects[fmt.Sprintf("erasures/1001-%d", restored.UnixMicro())] = long
	store.objects[fmt.Sprintf("erasures/1002-%d", restored.UnixMicro())] = long

	erased, err := repo.ReplayErasures(ctx, time.Now())

	require.NoError(t, err)
	assert.Equal(t, 1, erased)
	remaining := client.User.Query().Order(ent.Asc(user.FieldID)).IDsX(ctx)
	assert.Equal(t, []uint64{1002, 1003}, remaining, "restored accounts are erased and newer incarnations survive")
	var deleted data.UserDeletedDTO
	require.Len(t, pub.On(data.SubjectUserDeleted), 1)
	require.NoError(t, codec.Unmarshal(pub.On(data.SubjectUserDeleted)[0].Payload, &deleted))
	assert.Equal(t, []any{uint64(1001), restored.UnixMicro(), false}, []any{deleted.UserID, deleted.AccountCreatedAt, deleted.DeletedAt.IsZero()})

	again, err := repo.ReplayErasures(ctx, time.Now())
	require.NoError(t, err)
	assert.Zero(t, again)
}

func TestReplayErasuresWaitsForInFlightDeletes(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	store := newFakeErasureStore()
	repo.SetErasureStore(store)

	created := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	client.User.Create().SetID(1001).SetUsername("deleting").SetEmail("a@example.com").SetCreatedAt(created).SaveX(ctx)
	store.objects[fmt.Sprintf("erasures/1001-%d", created.UnixMicro())] = time.Now()

	erased, err := repo.ReplayErasures(ctx, time.Now())

	require.NoError(t, err)
	assert.Zero(t, erased)
	assert.True(t, client.User.Query().Where(user.IDEQ(1001)).ExistX(ctx))
}

func TestReplayErasuresResendsWithCommittedDeletionTime(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()
	store := newFakeErasureStore()
	repo.SetErasureStore(store)

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "Mavey", "mavey@example.com"))
	require.NoError(t, repo.Delete(ctx, 1001))
	committed := client.LifecycleErasure.Query().OnlyX(ctx).DeletedAt
	client.LifecycleErasure.Update().ClearDeletedPublishedAt().ExecX(ctx)
	for key := range store.objects {
		store.objects[key] = committed.Add(-2 * time.Minute)
	}

	_, err := repo.ReplayErasures(ctx, time.Now())

	require.NoError(t, err)
	events := pub.On(data.SubjectUserDeleted)
	require.Len(t, events, 2)
	var resent data.UserDeletedDTO
	require.NoError(t, codec.Unmarshal(events[1].Payload, &resent))
	assert.True(t, resent.DeletedAt.Equal(committed), "a revoke-delayed commit must not shrink the consumers' cutoff")
}

func TestReplayErasuresResendsOnlyPendingDeletionEvents(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()
	store := newFakeErasureStore()
	repo.SetErasureStore(store)

	recorded := time.Now().Add(-30 * time.Minute).UTC()
	for _, row := range []struct {
		id        uint64
		age       time.Duration
		marker    bool
		done      bool
		published bool
	}{
		{1001, 30 * time.Minute, true, false, false},
		{1002, 10 * 24 * time.Hour, true, true, false},
		{1003, 30 * time.Minute, true, true, true},
		{1004, 10 * 24 * time.Hour, true, false, true},
		{1005, 30 * time.Minute, false, false, false},
	} {
		store.objects[fmt.Sprintf("erasures/%d-111", row.id)] = time.Now().Add(-row.age)
		if !row.marker {
			continue
		}
		q := client.LifecycleErasure.Create().SetUserID(row.id).SetAccountCreatedAt(111).SetDeletedAt(recorded)
		if row.done {
			q.SetCompletedAt(time.Now().Add(-row.age))
		}
		if row.published {
			q.SetDeletedPublishedAt(recorded)
		}
		q.SaveX(ctx)
	}

	erased, err := repo.ReplayErasures(ctx, time.Now())

	require.NoError(t, err)
	assert.Zero(t, erased)
	events := pub.On(data.SubjectUserDeleted)
	require.Len(t, events, 2)
	for i, event := range events {
		var resent data.UserDeletedDTO
		require.NoError(t, codec.Unmarshal(event.Payload, &resent))
		assert.Equal(t, uint64(1001+i), resent.UserID)
		assert.True(t, resent.DeletedAt.Equal(recorded))
	}
}
