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

	assert.Equal(t, []string{fmt.Sprintf("erasures/1001-%d", createdAt)}, store.keys())
	assert.ElementsMatch(t, []string{"broadcaster-tok", "user-tok"}, revoker.revoked)
	assert.Equal(t, 0, client.User.Query().CountX(ctx))
	assert.Len(t, pub.On(data.SubjectUserDeleted), 1)
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
	assert.False(t, client.User.Query().Where(user.IDEQ(1001)).ExistX(ctx))
	assert.True(t, client.User.Query().Where(user.IDEQ(1002)).ExistX(ctx), "a newer account under the same id is a different person")
	assert.True(t, client.User.Query().Where(user.IDEQ(1003)).ExistX(ctx))
	var deleted data.UserDeletedDTO
	require.Len(t, pub.On(data.SubjectUserDeleted), 1)
	require.NoError(t, codec.Unmarshal(pub.On(data.SubjectUserDeleted)[0].Payload, &deleted))
	assert.Equal(t, data.UserDeletedDTO{UserID: 1001, AccountCreatedAt: restored.UnixMicro()}, deleted)

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

func TestReplayErasuresResendsRecentlyLostDeletionEvents(t *testing.T) {
	_, pub, repo := setup(t)
	ctx := context.Background()
	store := newFakeErasureStore()
	repo.SetErasureStore(store)

	recorded := time.Now().Add(-30 * time.Minute).UTC()
	store.objects["erasures/1001-111"] = recorded
	store.objects["erasures/1002-222"] = time.Now().Add(-3 * time.Hour)

	erased, err := repo.ReplayErasures(ctx, time.Now())

	require.NoError(t, err)
	assert.Zero(t, erased)
	require.Len(t, pub.On(data.SubjectUserDeleted), 1)
	var resent data.UserDeletedDTO
	require.NoError(t, codec.Unmarshal(pub.On(data.SubjectUserDeleted)[0].Payload, &resent))
	assert.Equal(t, uint64(1001), resent.UserID)
	assert.Equal(t, int64(111), resent.AccountCreatedAt)
	assert.True(t, resent.DeletedAt.Equal(recorded), "the record time fences consumers that erase by deletion time")
}
