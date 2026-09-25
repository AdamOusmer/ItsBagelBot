// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"strings"
	"testing"
	"unicode/utf8"

	"ItsBagelBot/internal/domain/event/data"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBumpNameColumnLimit(t *testing.T) {
	cases := []struct {
		name   string
		usable bool
	}{
		{strings.Repeat("a", maxCounterName), true},
		{strings.Repeat("a", maxCounterName+1), false},
		{strings.Repeat("é", maxCounterName), true},
		{strings.Repeat("é", maxCounterName+1), false},
		{strings.Repeat("🥯", maxCounterName), true},
		{"!" + strings.Repeat("A", maxCounterName) + " ", true},
	}
	for _, tc := range cases {
		_, _, ok := bumpTarget(1, data.CounterBumpEntry{Name: tc.name, Delta: 1})
		assert.Equal(t, tc.usable, ok, "len=%d runes=%d", len(tc.name), utf8.RuneCountInString(tc.name))
	}

	key, _, ok := bumpTarget(1, data.CounterBumpEntry{Name: "A\xffb", Delta: 1})
	require.True(t, ok)
	assert.True(t, utf8.ValidString(key.name))
}

func TestBumpCommandTruncatesToColumnLimit(t *testing.T) {
	atLimit := strings.Repeat("€", maxCounterName)
	over := strings.Repeat("€", maxCounterName+1)
	for _, scope := range []string{data.CounterScopeCommand, data.CounterScopeViewerCommand} {
		key, _, ok := bumpTarget(1, data.CounterBumpEntry{Name: "uses", Scope: scope, ViewerID: 7, Command: "!" + atLimit, Delta: 1})
		require.True(t, ok, scope)
		assert.Equal(t, atLimit, key.command, scope)

		key, _, ok = bumpTarget(1, data.CounterBumpEntry{Name: "uses", Scope: scope, ViewerID: 7, Command: over, Delta: 1})
		require.True(t, ok, scope)
		assert.Equal(t, atLimit, key.command, scope)
	}

	key, scope, ok := bumpTarget(1, data.CounterBumpEntry{Name: "deaths", Command: over, Delta: 1})
	require.True(t, ok)
	assert.Equal(t, data.CounterScopeChannel, scope)
	assert.Empty(t, key.command)
}

func TestNormalizeCommandTruncatesCharacters(t *testing.T) {
	got := normalizeCommand("!" + strings.Repeat("€", maxCounterName+5))
	assert.True(t, utf8.ValidString(got))
	assert.Equal(t, maxCounterName, utf8.RuneCountInString(got))
	assert.Equal(t, "hug", normalizeCommand(" !Hug "))
}

func TestOverLongViewerDisplayKeepsDelta(t *testing.T) {
	long := strings.Repeat("x", maxCounterName+1)
	r := bare()
	r.RecordBumps(data.CounterBumpedDTO{UserID: 1, Bumps: []data.CounterBumpEntry{
		{Name: "hugs", Scope: data.CounterScopeViewer, ViewerID: 7, ViewerLogin: "cool", ViewerName: "Cool", Delta: 1},
		{Name: "hugs", Scope: data.CounterScopeViewer, ViewerID: 7, ViewerLogin: long, ViewerName: "a\xff", Delta: 2},
		{Name: "hugs", Scope: data.CounterScopeViewer, ViewerID: 8, ViewerLogin: long, ViewerName: strings.Repeat("é", maxCounterName), Delta: 3},
	}})
	_, bumps := r.drain()
	require.Len(t, bumps, 2)
	seven := bumps[bumpKey{userID: 1, name: "hugs", viewerID: 7}]
	require.NotNil(t, seven)
	assert.Equal(t, int64(3), seven.delta)
	assert.Equal(t, "cool", seven.login)
	assert.Equal(t, "Cool", seven.name)
	eight := bumps[bumpKey{userID: 1, name: "hugs", viewerID: 8}]
	require.NotNil(t, eight)
	assert.Equal(t, int64(3), eight.delta)
	assert.Empty(t, eight.login)
	assert.Equal(t, strings.Repeat("é", maxCounterName), eight.name)
}

func TestOverLongEarnDisplayKeepsPoints(t *testing.T) {
	long := strings.Repeat("🥯", maxCounterName+1)
	r := bare()
	r.RecordEarned(data.LoyaltyEarnedDTO{UserID: 1, Entries: []data.LoyaltyEarnEntry{
		{ViewerID: 7, ViewerLogin: "cool", ViewerName: "Cool", Points: 10},
		{ViewerID: 7, ViewerLogin: long, ViewerName: long, Points: 5, WatchSeconds: 60},
		{ViewerID: 8, ViewerLogin: long, Points: 1},
	}})
	earn, _ := r.drain()
	require.Len(t, earn, 2)
	seven := earn[balKey{userID: 1, viewerID: 7}]
	require.NotNil(t, seven)
	assert.Equal(t, int64(15), seven.points)
	assert.Equal(t, uint64(60), seven.watchSeconds)
	assert.Equal(t, "cool", seven.login)
	assert.Equal(t, "Cool", seven.name)
	eight := earn[balKey{userID: 1, viewerID: 8}]
	require.NotNil(t, eight)
	assert.Equal(t, int64(1), eight.points)
	assert.Empty(t, eight.login)
}
