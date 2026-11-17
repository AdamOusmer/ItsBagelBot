// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/variable"
	"ItsBagelBot/app/db/modules/repository"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
)

func TestVariableSQLiteVanishedUpdateReturnsNotFound(t *testing.T) {
	seed, raw := sqliteStateRepo(t)
	require.NoError(t, seed.VariableSet(t.Context(), viewerVars, "score", "0"))
	drv := entsql.OpenDB("sqlite3", raw)
	client := ent.NewClient(ent.Driver(drv))
	client.Variable.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Op().Is(ent.OpUpdate | ent.OpUpdateOne) {
				_, err := m.(*ent.VariableMutation).Client().Variable.Delete().Where(variable.NameEQ("score")).Exec(ctx)
				require.NoError(t, err)
			}
			return next.Mutate(ctx, m)
		})
	})
	repo := repository.NewRecipeState(client, drv)
	err := repo.VariableAdd(t.Context(), viewerVars, "score", 1)
	require.True(t, ent.IsNotFound(err), "a vanished row must not complete an input successfully")
}
