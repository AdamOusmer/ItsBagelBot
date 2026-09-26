// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"ItsBagelBot/app/db/modules/ent/modules"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

func TestLoyaltyCapturedInstanceDoesNotReviveRecreatedAccount(t *testing.T) {
	client, _, repo := setup(t)
	epoch := int64(100)
	repo.SetAccountInstanceResolver(func(context.Context, uint64) (int64, error) { return epoch, nil })
	require.NoError(t, repo.Set(1001, "loyalty", true, codec.RawMessage(`{"watchtime_points":10,"__account_created_at":999}`)))
	epoch = 101
	repo.Close(t.Context())
	require.Zero(t, client.Modules.Query().CountX(t.Context()))
}
func TestLoyaltyMetadataIsServerOwnedAndPublished(t *testing.T) {
	client, pub, repo := setup(t)
	repo.SetAccountInstanceResolver(func(context.Context, uint64) (int64, error) { return 100, nil })
	require.NoError(t, repo.Set(1001, "loyalty", true, codec.RawMessage(`{"__account_created_at":999}`)))
	repo.Close(t.Context())
	row := client.Modules.Query().OnlyX(t.Context())
	var cfg map[string]codec.RawMessage
	require.NoError(t, codec.Unmarshal(row.Configs, &cfg))
	require.JSONEq(t, "100", string(cfg["__account_created_at"]))
	events := pub.On(data.SubjectModuleChanged)
	require.Len(t, events, 1)
	var dto data.ModuleChangedDTO
	require.NoError(t, codec.Unmarshal(events[0].Payload, &dto))
	require.Equal(t, int64(100), dto.AccountCreatedAt)
}
func TestLoyaltyPatchReplacesOldOwnerAndOldDeleteKeepsNewOwner(t *testing.T) {
	client, _, repo := setup(t)
	defer repo.Close(t.Context())
	client.Modules.Create().SetUserID(1001).SetName("loyalty").SetConfigs([]byte(`{"__account_created_at":100,"old_secret":"old"}`)).SetRevision(1).SaveX(t.Context())
	repo.SetAccountInstanceResolver(func(context.Context, uint64) (int64, error) { return 101, nil })
	_, err := repo.Patch(t.Context(), 1001, "loyalty", true, map[string]codec.RawMessage{"__account_created_at": codec.RawMessage(`999`)}, nil)
	require.NoError(t, err)
	row := client.Modules.Query().OnlyX(t.Context())
	var cfg map[string]codec.RawMessage
	require.NoError(t, codec.Unmarshal(row.Configs, &cfg))
	require.NotContains(t, cfg, "old_secret")
	require.JSONEq(t, "101", string(cfg["__account_created_at"]))
	require.NoError(t, repo.DeleteAccount(t.Context(), 1001, 100))
	require.Equal(t, 1, client.Modules.Query().CountX(t.Context()))
	require.NoError(t, repo.DeleteAccount(t.Context(), 1001, 101))
	require.Zero(t, client.Modules.Query().CountX(t.Context()))
}
func TestLoyaltyUnavailableSourceRejectsWritesAndLegacyReadIsClosed(t *testing.T) {
	client, _, repo := setup(t)
	defer repo.Close(t.Context())
	client.Modules.Create().SetUserID(1001).SetName("loyalty").SetConfigs([]byte(`{}`)).SaveX(t.Context())
	client.Modules.Create().SetUserID(1001).SetName("welcome").SetConfigs([]byte(`{}`)).SaveX(t.Context())
	repo.SetAccountInstanceResolver(func(context.Context, uint64) (int64, error) { return 0, errors.New("authority down") })
	require.Error(t, repo.Set(1001, "loyalty", true, codec.RawMessage(`{}`)))
	_, err := repo.Patch(t.Context(), 1001, "loyalty", true, nil, nil)
	require.Error(t, err)
	views, err := repo.List(t.Context(), 1001)
	require.NoError(t, err)
	require.Len(t, views, 1)
	require.Equal(t, "welcome", views[0].Name)
	require.Equal(t, 2, client.Modules.Query().Where(modules.UserIDEQ(1001)).CountX(t.Context()))
}

func TestOldAndUnknownDeletionPreserveEveryRecreatedSection(t *testing.T) {
	for _, deleted := range []int64{0, 100} {
		t.Run(fmt.Sprint(deleted), func(t *testing.T) {
			client, _, repo := setup(t)
			defer repo.Close(t.Context())
			ctx := t.Context()
			client.Modules.Create().SetUserID(1001).SetName("welcome").SetConfigs([]byte(`{}`)).SaveX(ctx)
			client.Quote.Create().SetUserID(1001).SetNumber(1).SetText("replacement quote").SaveX(ctx)
			client.GoveeCredential.Create().SetUserID(1001).SetKeyEnc([]byte("replacement-key")).SaveX(ctx)
			client.SpotifyCredential.Create().SetUserID(1001).SetTokenEnc([]byte("replacement-token")).SaveX(ctx)
			called := false
			repo.SetAccountInstanceResolver(func(context.Context, uint64) (int64, error) { called = true; return 101, nil })
			require.NoError(t, repo.DeleteAccount(ctx, 1001, deleted))
			require.Equal(t, deleted > 0, called)
			require.Equal(t, 1, client.Modules.Query().CountX(ctx))
			require.Equal(t, 1, client.Quote.Query().CountX(ctx))
			require.Equal(t, 1, client.GoveeCredential.Query().CountX(ctx))
			require.Equal(t, 1, client.SpotifyCredential.Query().CountX(ctx))
		})
	}
}

func TestDeletionAuthorityFailurePreservesEverySection(t *testing.T) {
	client, _, repo := setup(t)
	defer repo.Close(t.Context())
	ctx := t.Context()
	client.Modules.Create().SetUserID(1001).SetName("welcome").SetConfigs([]byte(`{}`)).SaveX(ctx)
	client.GoveeCredential.Create().SetUserID(1001).SetKeyEnc([]byte("key")).SaveX(ctx)
	client.SpotifyCredential.Create().SetUserID(1001).SaveX(ctx)
	require.NoError(t, repo.DeleteAccount(ctx, 1001, 100)) // No authority configured: fail closed.
	repo.SetAccountInstanceResolver(func(context.Context, uint64) (int64, error) { return 0, errors.New("authority down") })
	require.ErrorContains(t, repo.DeleteAccount(ctx, 1001, 100), "authority down")
	require.Equal(t, 1, client.Modules.Query().CountX(ctx))
	require.Equal(t, 1, client.GoveeCredential.Query().CountX(ctx))
	require.Equal(t, 1, client.SpotifyCredential.Query().CountX(ctx))
}

func TestDeletionPreservesRowsUpdatedDuringCanonicalCheck(t *testing.T) {
	client, _, repo := setup(t)
	defer repo.Close(t.Context())
	ctx := t.Context()
	mod := client.Modules.Create().SetUserID(1001).SetName("welcome").SetConfigs([]byte(`{}`)).SetRevision(1).SaveX(ctx)
	key := client.GoveeCredential.Create().SetUserID(1001).SetKeyEnc([]byte("old-key")).SaveX(ctx)
	token := client.SpotifyCredential.Create().SetUserID(1001).SetTokenEnc([]byte("old-token")).SaveX(ctx)
	quote := client.Quote.Create().SetUserID(1001).SetNumber(1).SetText("old quote").SaveX(ctx)
	repo.SetAccountInstanceResolver(func(context.Context, uint64) (int64, error) {
		require.NoError(t, client.Modules.UpdateOneID(mod.ID).AddRevision(1).SetConfigs([]byte(`{"message":"fresh"}`)).Exec(ctx))
		require.NoError(t, client.GoveeCredential.UpdateOneID(key.ID).SetKeyEnc([]byte("fresh-key")).Exec(ctx))
		require.NoError(t, client.SpotifyCredential.UpdateOneID(token.ID).SetTokenEnc([]byte("fresh-token")).Exec(ctx))
		require.NoError(t, client.Quote.UpdateOneID(quote.ID).SetText("fresh quote").Exec(ctx))
		return 0, nil
	})
	require.NoError(t, repo.DeleteAccount(ctx, 1001, 100))
	require.Equal(t, 2, client.Modules.Query().OnlyX(ctx).Revision)
	require.Equal(t, []byte("fresh-key"), client.GoveeCredential.Query().OnlyX(ctx).KeyEnc)
	require.Equal(t, []byte("fresh-token"), client.SpotifyCredential.Query().OnlyX(ctx).TokenEnc)
	require.Equal(t, "fresh quote", client.Quote.Query().OnlyX(ctx).Text)
}

func TestCurrentDeletionRemovesCapturedRowsAndCredentials(t *testing.T) {
	client, _, repo := setup(t)
	defer repo.Close(t.Context())
	ctx := t.Context()
	client.Modules.Create().SetUserID(1001).SetName("welcome").SetConfigs([]byte(`{}`)).SaveX(ctx)
	client.Quote.Create().SetUserID(1001).SetNumber(1).SetText("old quote").SaveX(ctx)
	client.GoveeCredential.Create().SetUserID(1001).SetKeyEnc([]byte("key")).SaveX(ctx)
	client.SpotifyCredential.Create().SetUserID(1001).SaveX(ctx)
	repo.SetAccountInstanceResolver(func(context.Context, uint64) (int64, error) { return 0, nil })
	require.NoError(t, repo.DeleteAccount(ctx, 1001, 100))
	require.Zero(t, client.Modules.Query().CountX(ctx))
	require.Zero(t, client.Quote.Query().CountX(ctx))
	require.Zero(t, client.GoveeCredential.Query().CountX(ctx))
	require.Zero(t, client.SpotifyCredential.Query().CountX(ctx))
}
