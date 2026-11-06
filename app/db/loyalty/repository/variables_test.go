// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/automation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	channelVars = loyaltyrepo.VariableOwner{UserID: 2, Scope: automation.VariableScopeChannel}
	viewerVars  = loyaltyrepo.VariableOwner{UserID: 2, ViewerID: 7, Scope: automation.VariableScopeViewer}
)

func variableValue(t *testing.T, repo *loyaltyrepo.Loyalty, owner loyaltyrepo.VariableOwner, name string) (string, bool) {
	t.Helper()
	values, err := repo.VariablesGet(context.Background(), owner, []string{name})
	require.NoError(t, err)
	value, ok := values[strings.ToLower(name)]
	return value, ok
}

func TestVariableSetAddDeleteAcrossScopes(t *testing.T) {
	repo, _ := newLoyaltyRepo(t)
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
	repo, raw := sqliteRetentionRepo(t)
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
	repo, _ := newLoyaltyRepo(t)
	ctx := context.Background()
	require.NoError(t, repo.VariableSet(ctx, channelVars, "big", "9223372036854775807"))
	assert.ErrorIs(t, repo.VariableAdd(ctx, channelVars, "big", 1), loyaltyrepo.ErrVariableLimit)

	assert.NoError(t, repo.VariableSet(ctx, channelVars, "text", strings.Repeat("é", automation.MaxVariableValueRunes)))
	value, _ := variableValue(t, repo, channelVars, "text")
	assert.Equal(t, strings.Repeat("é", automation.MaxVariableValueRunes), value)
	assert.ErrorIs(t, repo.VariableSet(ctx, channelVars, "text", strings.Repeat("é", automation.MaxVariableValueRunes+1)), loyaltyrepo.ErrVariableLimit)
}

func TestVariableNameLimitsPerScope(t *testing.T) {
	repo, _ := newLoyaltyRepo(t)
	ctx := context.Background()
	for i := range automation.MaxChannelVariables {
		require.NoError(t, repo.VariableSet(ctx, channelVars, fmt.Sprintf("n%d", i), "x"))
	}
	assert.ErrorIs(t, repo.VariableSet(ctx, channelVars, "over", "x"), loyaltyrepo.ErrVariableLimit)
	assert.NoError(t, repo.VariableSet(ctx, channelVars, "n0", "updating is fine"))
	assert.NoError(t, repo.VariableSet(ctx, viewerVars, "over", "viewer names count separately"))

	otherViewer := viewerVars
	otherViewer.ViewerID = 8
	assert.NoError(t, repo.VariableSet(ctx, otherViewer, "over", "a name already in use takes no new slot"))
}

func TestVariableInvalidInputIsRefused(t *testing.T) {
	repo, _ := newLoyaltyRepo(t)
	ctx := context.Background()
	for name, owner := range map[string]loyaltyrepo.VariableOwner{
		"no channel":       {Scope: automation.VariableScopeChannel},
		"bad scope":        {UserID: 2, Scope: "global"},
		"viewer on chan":   {UserID: 2, ViewerID: 7, Scope: automation.VariableScopeChannel},
		"no viewer on vwr": {UserID: 2, Scope: automation.VariableScopeViewer},
	} {
		assert.ErrorIs(t, repo.VariableSet(ctx, owner, "x", "y"), loyaltyrepo.ErrInvalidInput, name)
	}
	assert.ErrorIs(t, repo.VariableSet(ctx, channelVars, "bad name", "y"), loyaltyrepo.ErrInvalidInput)
	_, err := repo.VariablesGet(ctx, channelVars, []string{"bad name"})
	assert.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)
}

func TestDeleteAccountRemovesVariables(t *testing.T) {
	repo, _ := sqliteRetentionRepo(t)
	ctx := context.Background()
	require.NoError(t, repo.VariableSet(ctx, channelVars, "x", "y"))
	require.NoError(t, repo.VariableSet(ctx, viewerVars, "x", "y"))
	require.NoError(t, repo.DeleteAccount(ctx, 2, 0))
	_, ok := variableValue(t, repo, channelVars, "x")
	assert.False(t, ok)
	_, ok = variableValue(t, repo, viewerVars, "x")
	assert.False(t, ok)
}
