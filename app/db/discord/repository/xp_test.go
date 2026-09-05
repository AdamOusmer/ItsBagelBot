// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/discord/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXPAddCreatesThenAccumulates(t *testing.T) {
	repo, ctx := newStore(t, "xpadd")

	result, err := repo.XPAdd(ctx, "g1", "u1", 15)
	require.NoError(t, err)
	assert.Equal(t, int64(15), result.XP)
	assert.Equal(t, 0, result.Level)
	assert.False(t, result.LeveledUp)

	result, err = repo.XPAdd(ctx, "g1", "u1", 15)
	require.NoError(t, err)
	assert.Equal(t, int64(30), result.XP)

	row, found, err := repo.XPGet(ctx, "g1", "u1")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, int64(30), row.Xp)
}

func TestXPAddReportsTheLevelBoundaryOnce(t *testing.T) {
	repo, ctx := newStore(t, "xplevel")

	result, err := repo.XPAdd(ctx, "g1", "u1", 99)
	require.NoError(t, err)
	require.False(t, result.LeveledUp)

	// 99 -> 100 crosses into level 1 (the curve is level = floor(sqrt(xp/100))).
	result, err = repo.XPAdd(ctx, "g1", "u1", 1)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Level)
	assert.True(t, result.LeveledUp)

	result, err = repo.XPAdd(ctx, "g1", "u1", 1)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Level)
	assert.False(t, result.LeveledUp, "staying inside a level must not re-announce it")

	row, _, err := repo.XPGet(ctx, "g1", "u1")
	require.NoError(t, err)
	assert.Equal(t, 1, row.Level, "the stored level column tracks the curve")
}

func TestXPIsScopedPerGuildAndMember(t *testing.T) {
	repo, ctx := newStore(t, "xpscope")

	_, err := repo.XPAdd(ctx, "g1", "u1", 100)
	require.NoError(t, err)
	_, err = repo.XPAdd(ctx, "g2", "u1", 5)
	require.NoError(t, err)

	row, _, err := repo.XPGet(ctx, "g2", "u1")
	require.NoError(t, err)
	assert.Equal(t, int64(5), row.Xp)

	_, found, err := repo.XPGet(ctx, "g1", "u2")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestXPGetMissingIsNotAnError(t *testing.T) {
	repo, ctx := newStore(t, "xpmissing")

	row, found, err := repo.XPGet(ctx, "g1", "nobody")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, row)
}

func TestXPRejectsEmptyIdentifiers(t *testing.T) {
	repo, ctx := newStore(t, "xpinvalid")

	_, err := repo.XPAdd(ctx, "", "u1", 1)
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
	_, err = repo.XPDaily(ctx, "g1", "", 1)
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
	_, _, err = repo.XPGet(ctx, "g1", "")
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
	_, err = repo.XPTop(ctx, "", 5)
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
}

func TestXPDailyGrantsOnceInsideTheWindow(t *testing.T) {
	repo, ctx := newStore(t, "xpdaily")

	first, err := repo.XPDaily(ctx, "g1", "u1", 50)
	require.NoError(t, err)
	assert.True(t, first.Granted)
	assert.Equal(t, int64(50), first.XP)
	assert.WithinDuration(t, time.Now().Add(repository.DailyWindow), first.NextDaily, time.Minute)

	second, err := repo.XPDaily(ctx, "g1", "u1", 50)
	require.NoError(t, err)
	assert.False(t, second.Granted)
	assert.Equal(t, int64(50), second.XP, "a refused claim must not credit anything")
	assert.WithinDuration(t, first.NextDaily, second.NextDaily, time.Second)
}

func TestXPDailyGrantsAgainOnceTheWindowPasses(t *testing.T) {
	repo, ctx := newStore(t, "xpdailywindow")

	_, err := repo.XPDaily(ctx, "g1", "u1", 50)
	require.NoError(t, err)

	// Reach past the repository to age the stored claim: the alternative is a
	// test that sleeps for 24 hours.
	row, _, err := repo.XPGet(ctx, "g1", "u1")
	require.NoError(t, err)
	require.NoError(t, repo.AgeLastDaily(ctx, row.ID, repository.DailyWindow+time.Minute))

	again, err := repo.XPDaily(ctx, "g1", "u1", 50)
	require.NoError(t, err)
	assert.True(t, again.Granted)
	assert.Equal(t, int64(100), again.XP)
}

// TestXPDailyIsAtomicUnderConcurrentClaims is the property the 24h window
// exists for: several simultaneous /daily calls for one member must award the
// bonus exactly once, whichever of them wins.
func TestXPDailyIsAtomicUnderConcurrentClaims(t *testing.T) {
	repo, ctx := newConcurrentStore(t, "xpdailyrace")

	const callers = 8
	var wg sync.WaitGroup
	results := make([]repository.XPResult, callers)
	errs := make([]error, callers)
	wg.Add(callers)
	for i := range callers {
		go func() {
			defer wg.Done()
			results[i], errs[i] = repo.XPDaily(ctx, "g1", "u1", 50)
		}()
	}
	wg.Wait()

	granted := 0
	for i := range callers {
		require.NoError(t, errs[i])
		if results[i].Granted {
			granted++
		}
	}
	assert.Equal(t, 1, granted, "exactly one concurrent claim may be granted")

	row, _, err := repo.XPGet(ctx, "g1", "u1")
	require.NoError(t, err)
	assert.Equal(t, int64(50), row.Xp)
}

// TestXPAddIsAtomicUnderConcurrentWrites guards the read-modify-write: every
// concurrent delta must land, none may be lost to a stale read.
func TestXPAddIsAtomicUnderConcurrentWrites(t *testing.T) {
	repo, ctx := newConcurrentStore(t, "xpaddrace")

	const callers = 8
	var wg sync.WaitGroup
	errs := make([]error, callers)
	wg.Add(callers)
	for i := range callers {
		go func() {
			defer wg.Done()
			_, errs[i] = repo.XPAdd(ctx, "g1", "u1", 15)
		}()
	}
	wg.Wait()

	for i := range callers {
		require.NoError(t, errs[i])
	}
	row, _, err := repo.XPGet(ctx, "g1", "u1")
	require.NoError(t, err)
	assert.Equal(t, int64(callers*15), row.Xp)
}

func TestXPTopOrdersByXPAndCaps(t *testing.T) {
	repo, ctx := newStore(t, "xptop")

	for i := range 5 {
		_, err := repo.XPAdd(ctx, "g1", "u"+strconv.Itoa(i), int64((i+1)*100))
		require.NoError(t, err)
	}
	_, err := repo.XPAdd(ctx, "otherguild", "u9", 99999)
	require.NoError(t, err)

	rows, err := repo.XPTop(ctx, "g1", 3)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, "u4", rows[0].UserID)
	assert.Equal(t, int64(500), rows[0].Xp)
	assert.Equal(t, "u2", rows[2].UserID)

	all, err := repo.XPTop(ctx, "g1", 0)
	require.NoError(t, err)
	assert.Len(t, all, 5, "a zero limit falls back to the page size, not to nothing")
}
