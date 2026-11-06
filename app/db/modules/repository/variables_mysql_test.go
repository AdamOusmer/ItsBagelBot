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

	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/automation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func concurrently(workers int, work func(ctx context.Context, i int) error) []error {
	start := make(chan struct{})
	errs := make([]error, workers)
	var group sync.WaitGroup
	for i := range workers {
		group.Go(func() {
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			errs[i] = work(ctx, i)
		})
	}
	close(start)
	group.Wait()
	return errs
}

func storedNames(t *testing.T, raw *sql.DB, user uint64, scope string) (names, rows int) {
	t.Helper()
	require.NoError(t, raw.QueryRow("SELECT COUNT(DISTINCT name), COUNT(*) FROM variables WHERE user_id = ? AND scope = ? AND name REGEXP '^[a-z0-9_]+$'", user, scope).Scan(&names, &rows))
	return names, rows
}

func TestVariableMySQLAddExactUnderConcurrentWriters(t *testing.T) {
	repo, raw := mysqlStateRepo(t)
	raw.SetMaxOpenConns(8)
	const writers = 100
	for _, tc := range []struct {
		owner  repository.VariableOwner
		amount int64
	}{{channelVars, 3}, {viewerVars, -1}} {
		errs := concurrently(writers, func(ctx context.Context, _ int) error {
			return repo.VariableAdd(ctx, tc.owner, "hits", tc.amount)
		})

		for _, err := range errs {
			require.NoError(t, err)
		}
		value, _ := variableValue(t, repo, tc.owner, "hits")
		assert.Equal(t, strconv.FormatInt(tc.amount*writers, 10), value)
	}
}

func checkNameLimitHolds(t *testing.T, repo *repository.RecipeState, raw *sql.DB, owner func(i int) repository.VariableOwner, scope string, limit int) {
	t.Helper()
	const attempts = 300
	errs := concurrently(attempts, func(ctx context.Context, i int) error {
		return repo.VariableSet(ctx, owner(i), fmt.Sprintf("n%d", i), "x")
	})
	admitted := 0
	for _, err := range errs {
		if err == nil {
			admitted++
			continue
		}
		require.ErrorIs(t, err, repository.ErrVariableLimit)
	}
	names, _ := storedNames(t, raw, owner(0).UserID, scope)
	assert.Equal(t, limit, names, "distinct names stored")
	assert.Equal(t, limit, admitted, "writes accepted")
}

func TestVariableMySQLChannelNameLimitHoldsUnderConcurrency(t *testing.T) {
	repo, raw := mysqlStateRepo(t)
	raw.SetMaxOpenConns(8)
	checkNameLimitHolds(t, repo, raw, func(int) repository.VariableOwner { return channelVars }, automation.VariableScopeChannel, automation.MaxChannelVariables)
}

func TestVariableMySQLViewerNameLimitHoldsAcrossViewers(t *testing.T) {
	repo, raw := mysqlStateRepo(t)
	raw.SetMaxOpenConns(8)
	checkNameLimitHolds(t, repo, raw, func(i int) repository.VariableOwner {
		return repository.VariableOwner{UserID: channelVars.UserID, ViewerID: uint64(i + 1), Scope: automation.VariableScopeViewer}
	}, automation.VariableScopeViewer, automation.MaxViewerVariableNames)

	var sharedName string
	require.NoError(t, raw.QueryRow("SELECT name FROM variables WHERE user_id = ? AND scope = ? AND name REGEXP '^[a-z0-9_]+$' LIMIT 1", channelVars.UserID, automation.VariableScopeViewer).Scan(&sharedName))
	ctx := context.Background()
	newViewer := repository.VariableOwner{UserID: channelVars.UserID, ViewerID: 999999, Scope: automation.VariableScopeViewer}
	assert.NoError(t, repo.VariableSet(ctx, newViewer, sharedName, "an existing name takes no slot"))
	assert.ErrorIs(t, repo.VariableSet(ctx, newViewer, "brand_new", "x"), repository.ErrVariableLimit)
	assert.NoError(t, repo.VariableSet(ctx, channelVars, "brand_new", "channel scope counts separately"))
}

func TestVariableMySQLViewerScopeIsolation(t *testing.T) {
	repo, raw := mysqlStateRepo(t)
	raw.SetMaxOpenConns(8)
	const viewers = 50
	viewer := func(i int) repository.VariableOwner {
		return repository.VariableOwner{UserID: 2, ViewerID: uint64(i + 1), Scope: automation.VariableScopeViewer}
	}

	errs := concurrently(viewers, func(ctx context.Context, i int) error {
		return repo.VariableAdd(ctx, viewer(i), "score", int64(i+1))
	})

	for _, err := range errs {
		require.NoError(t, err)
	}
	for i := range viewers {
		value, ok := variableValue(t, repo, viewer(i), "score")
		assert.True(t, ok)
		assert.Equal(t, strconv.Itoa(i+1), value)
	}
	_, ok := variableValue(t, repo, channelVars, "score")
	assert.False(t, ok, "a viewer value never shows in channel scope")
	otherChannel := repository.VariableOwner{UserID: 3, ViewerID: 1, Scope: automation.VariableScopeViewer}
	_, ok = variableValue(t, repo, otherChannel, "score")
	assert.False(t, ok, "a viewer value never crosses channels")
	require.NoError(t, repo.VariableDelete(context.Background(), viewer(0), "score"))
	_, rows := storedNames(t, raw, 2, automation.VariableScopeViewer)
	assert.Equal(t, viewers-1, rows, "delete touches one viewer")
}

func TestVariableMySQLLongEmojiValueSurvives(t *testing.T) {
	repo, raw := mysqlStateRepo(t)
	ctx := context.Background()
	fourBytes := strings.Repeat("\U0001F96F", automation.MaxVariableValueRunes)

	for name, value := range map[string]string{
		"emoji": fourBytes,
		"mixed": strings.Repeat("aé€\U0001F600", automation.MaxVariableValueRunes/4),
		"zwj":   strings.Repeat("\U0001F468‍\U0001F469‍\U0001F467", automation.MaxVariableValueRunes/5),
	} {
		require.NoError(t, repo.VariableSet(ctx, channelVars, name, value), name)
		got, ok := variableValue(t, repo, channelVars, name)
		assert.True(t, ok, name)
		assert.Equal(t, value, got, name)
	}
	assert.ErrorIs(t, repo.VariableSet(ctx, channelVars, "emoji", fourBytes+"\U0001F96F"), repository.ErrVariableLimit)

	var charset string
	var chars int
	require.NoError(t, raw.QueryRow("SELECT CHARACTER_SET_NAME, CHARACTER_MAXIMUM_LENGTH FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'variables' AND COLUMN_NAME = 'value'").Scan(&charset, &chars))
	assert.Equal(t, "utf8mb4", charset)
	assert.GreaterOrEqual(t, chars, automation.MaxVariableValueRunes)
	var stored int
	require.NoError(t, raw.QueryRow("SELECT LENGTH(value) FROM variables WHERE user_id = ? AND name = 'emoji'", channelVars.UserID).Scan(&stored))
	assert.Equal(t, 4*automation.MaxVariableValueRunes, stored, "no truncation at the byte level")

	require.NoError(t, repo.VariableAdd(ctx, channelVars, "emoji", 5))
	got, _ := variableValue(t, repo, channelVars, "emoji")
	assert.Equal(t, "5", got, "text counts as 0")
}
