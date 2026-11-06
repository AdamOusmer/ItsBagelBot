// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	_ "ItsBagelBot/app/db/modules/ent/runtime"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func stateRepoOn(t *testing.T, raw *sql.DB, dialectName string) *repository.RecipeState {
	t.Helper()
	drv := entsql.OpenDB(dialectName, raw)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, repository.MigrateSchema(context.Background(), client, dialectName))
	return repository.NewRecipeState(client, drv)
}

func sqliteStateRepo(t *testing.T) (*repository.RecipeState, *sql.DB) {
	t.Helper()
	raw, err := sql.Open(testdb.Driver, testdb.MemDSN(testdb.Name("recipestate"+strconv.FormatInt(time.Now().UnixNano(), 10))))
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	return stateRepoOn(t, raw, "sqlite3"), raw
}

func mysqlStateRepo(t *testing.T) (*repository.RecipeState, *sql.DB) {
	t.Helper()
	raw := testdb.MySQL(t, "bagel_recipe_state_")
	return stateRepoOn(t, raw, "mysql"), raw
}

var (
	channelVars = repository.VariableOwner{UserID: 2, Scope: automation.VariableScopeChannel}
	viewerVars  = repository.VariableOwner{UserID: 2, ViewerID: 7, Scope: automation.VariableScopeViewer}
)

func variableValue(t *testing.T, repo *repository.RecipeState, owner repository.VariableOwner, name string) (string, bool) {
	t.Helper()
	values, err := repo.VariablesGet(context.Background(), owner, []string{name})
	require.NoError(t, err)
	value, ok := values[strings.ToLower(name)]
	return value, ok
}

func TestVariableSetAddDeleteAcrossScopes(t *testing.T) {
	repo, _ := sqliteStateRepo(t)
	ctx := context.Background()
	require.NoError(t, repo.VariableSet(ctx, channelVars, "Deaths", "{arg:1}"))
	value, ok := variableValue(t, repo, channelVars, "DEATHS")
	assert.True(t, ok)
	assert.Equal(t, "{arg:1}", value, "stored text stays literal")

	require.NoError(t, repo.VariableAdd(ctx, channelVars, "deaths", 3))
	value, _ = variableValue(t, repo, channelVars, "deaths")
	assert.Equal(t, "3", value, "non-numeric counts as 0")

	_, ok = variableValue(t, repo, viewerVars, "deaths")
	assert.False(t, ok, "scopes do not share rows")

	require.NoError(t, repo.VariableDelete(ctx, channelVars, "deaths"))
	_, ok = variableValue(t, repo, channelVars, "deaths")
	assert.False(t, ok)
	require.NoError(t, repo.VariableDelete(ctx, channelVars, "deaths"), "deleting twice is fine")
}

func TestVariableAddIsAtomicUnderConcurrency(t *testing.T) {
	repo, raw := sqliteStateRepo(t)
	raw.SetMaxOpenConns(1)
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			assert.NoError(t, repo.VariableAdd(context.Background(), channelVars, "count", 1))
		})
	}
	group.Wait()
	value, _ := variableValue(t, repo, channelVars, "count")
	assert.Equal(t, "8", value)
}

func TestVariableOverflowAndValueLimits(t *testing.T) {
	repo, _ := sqliteStateRepo(t)
	ctx := context.Background()
	require.NoError(t, repo.VariableSet(ctx, channelVars, "big", "9223372036854775807"))
	assert.ErrorIs(t, repo.VariableAdd(ctx, channelVars, "big", 1), repository.ErrVariableLimit)

	assert.NoError(t, repo.VariableSet(ctx, channelVars, "text", strings.Repeat("é", automation.MaxVariableValueRunes)))
	value, _ := variableValue(t, repo, channelVars, "text")
	assert.Equal(t, strings.Repeat("é", automation.MaxVariableValueRunes), value)
	assert.ErrorIs(t, repo.VariableSet(ctx, channelVars, "text", strings.Repeat("é", automation.MaxVariableValueRunes+1)), repository.ErrVariableLimit)
}

func TestVariableNameLimitsPerScope(t *testing.T) {
	repo, _ := sqliteStateRepo(t)
	ctx := context.Background()
	for i := range automation.MaxChannelVariables {
		require.NoError(t, repo.VariableSet(ctx, channelVars, fmt.Sprintf("n%d", i), "x"))
	}
	assert.ErrorIs(t, repo.VariableSet(ctx, channelVars, "over", "x"), repository.ErrVariableLimit)
	assert.NoError(t, repo.VariableSet(ctx, channelVars, "n0", "updating is fine"))
	assert.NoError(t, repo.VariableSet(ctx, viewerVars, "over", "viewer names count separately"))

	otherViewer := viewerVars
	otherViewer.ViewerID = 8
	assert.NoError(t, repo.VariableSet(ctx, otherViewer, "over", "a name already in use takes no new slot"))
}

func TestVariableInvalidInputIsRefused(t *testing.T) {
	repo, _ := sqliteStateRepo(t)
	ctx := context.Background()
	for name, owner := range map[string]repository.VariableOwner{
		"no channel":       {Scope: automation.VariableScopeChannel},
		"bad scope":        {UserID: 2, Scope: "global"},
		"viewer on chan":   {UserID: 2, ViewerID: 7, Scope: automation.VariableScopeChannel},
		"no viewer on vwr": {UserID: 2, Scope: automation.VariableScopeViewer},
	} {
		assert.ErrorIs(t, repo.VariableSet(ctx, owner, "x", "y"), repository.ErrInvalidInput, name)
	}
	assert.ErrorIs(t, repo.VariableSet(ctx, channelVars, "bad name", "y"), repository.ErrInvalidInput)
	_, err := repo.VariablesGet(ctx, channelVars, []string{"bad name"})
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
}

func TestDeleteAccountRemovesVariablesAndCooldowns(t *testing.T) {
	const instance = 100
	for name, open := range map[string]func(*testing.T) (*repository.RecipeState, *sql.DB, string){
		"sqlite": func(t *testing.T) (*repository.RecipeState, *sql.DB, string) {
			repo, raw := sqliteStateRepo(t)
			return repo, raw, "sqlite3"
		},
		"mysql": func(t *testing.T) (*repository.RecipeState, *sql.DB, string) {
			repo, raw := mysqlStateRepo(t)
			return repo, raw, "mysql"
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo, raw, dialectName := open(t)
			ctx := context.Background()
			require.NoError(t, repo.VariableSet(ctx, channelVars, "x", "y"))
			require.NoError(t, repo.VariableSet(ctx, viewerVars, "x", "y"))
			other := repository.VariableOwner{UserID: channelVars.UserID + 1, Scope: automation.VariableScopeChannel}
			require.NoError(t, repo.VariableSet(ctx, other, "x", "kept"))
			_, won, err := repo.CooldownClaimAll(ctx, repository.CooldownClaim{UserID: channelVars.UserID, Windows: []repository.CooldownWindow{window("mine")}})
			require.NoError(t, err)
			require.True(t, won)
			_, won, err = repo.CooldownClaimAll(ctx, repository.CooldownClaim{UserID: other.UserID, Windows: []repository.CooldownWindow{window("theirs")}})
			require.NoError(t, err)
			require.True(t, won)

			modulesRepo := repository.NewModules(ent.NewClient(ent.Driver(entsql.OpenDB(dialectName, raw))), bustest.NewPublisher(), nil, zap.NewNop())
			t.Cleanup(func() { modulesRepo.Close(context.Background()) })
			modulesRepo.SetAccountInstanceResolver(func(context.Context, uint64) (int64, error) { return instance, nil })
			require.NoError(t, modulesRepo.DeleteAccount(ctx, channelVars.UserID, instance))

			_, ok := variableValue(t, repo, channelVars, "x")
			assert.False(t, ok)
			_, ok = variableValue(t, repo, viewerVars, "x")
			assert.False(t, ok)
			value, _ := variableValue(t, repo, other, "x")
			assert.Equal(t, "kept", value)
			assert.Equal(t, []string{"theirs"}, cooldownRows(t, raw))
		})
	}
}
