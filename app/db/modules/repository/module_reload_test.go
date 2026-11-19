// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/event/data"
	"github.com/stretchr/testify/require"
)

func TestCommittedModuleReloadFailureEvictsCachedList(t *testing.T) {
	client, pub, repo := setup(t)
	t.Cleanup(func() { repo.Close(context.Background()) })
	client.Modules.Create().SetUserID(1001).SetName("welcome").SetIsEnabled(true).ExecX(t.Context())
	views, err := repo.List(t.Context(), 1001)
	require.NoError(t, err)
	require.True(t, views[0].IsEnabled)
	failure := errors.New("reload failed")
	fail := false
	client.Modules.Use(armModuleReloadFailure(&fail))
	client.Modules.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			if fail {
				fail = false
				return nil, failure
			}
			return next.Query(ctx, query)
		})
	}))
	_, err = repo.SetNow(t.Context(), repository.ModuleWrite{UserID: 1001, Name: "welcome", Enabled: false})
	require.ErrorIs(t, err, failure)
	require.Empty(t, pub.On(data.SubjectModuleChanged))
	views, err = repo.List(t.Context(), 1001)
	require.NoError(t, err)
	require.False(t, views[0].IsEnabled)
}

func armModuleReloadFailure(fail *bool) ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			tx, err := mutation.(*ent.ModulesMutation).Tx()
			if err != nil {
				return nil, err
			}
			tx.OnCommit(func(next ent.Committer) ent.Committer {
				return ent.CommitFunc(func(ctx context.Context, tx *ent.Tx) error {
					err := next.Commit(ctx, tx)
					*fail = err == nil
					return err
				})
			})
			return next.Mutate(ctx, mutation)
		})
	}
}
