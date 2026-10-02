// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"strings"
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/event/data"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidCounterName(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "normalizes the bang prefix, case and padding", input: "  !Deaths ", want: "deaths"},
		{name: "rejects a blank name", input: "   ", wantErr: true},
		{name: "rejects a name over the column limit", input: strings.Repeat("a", 65), wantErr: true},
		{name: "rejects the reserved namespace separator", input: "target:deaths", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := loyaltyrepo.ValidCounterName(tc.input)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantErr, err != nil)
			if tc.wantErr {
				assert.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)
			}
		})
	}
}

func TestValidScope(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "defaults an empty scope to the channel", want: data.CounterScopeChannel},
		{name: "keeps a known scope", input: data.CounterScopeViewer, want: data.CounterScopeViewer},
		{name: "rejects an unknown scope", input: "global", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := loyaltyrepo.ValidScope(tc.input)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantErr, err != nil)
			if tc.wantErr {
				assert.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)
			}
		})
	}
}

func TestCounterCreateScopesAndReservedNames(t *testing.T) {
	for _, tc := range []struct {
		name     string
		userID   uint64
		counter  string
		scope    string
		wantName string
	}{
		{name: "refuses a system counter in a channel", userID: 123, counter: data.CounterMessagesProcessed},
		{name: "refuses a system counter after normalization", userID: 123, counter: "  !Messages_Processed "},
		{name: "allows system counters in the bot namespace", counter: data.CounterEventsProcessed, scope: data.CounterScopeBot, wantName: data.CounterEventsProcessed},
		{name: "allows ordinary channel counters", userID: 123, counter: "deaths", wantName: "deaths"},
		{name: "refuses a bot scope outside the bot namespace", userID: 123, counter: "deaths", scope: data.CounterScopeBot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _ := newLoyaltyRepo(t)
			row, err := repo.CounterCreate(t.Context(), tc.userID, tc.counter, tc.scope)
			if tc.wantName == "" {
				require.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantName, row.Name)
		})
	}
}

func TestTrialCountersAreReservedSystemCounters(t *testing.T) {
	require.True(t, data.SystemCounter(data.CounterTrialDecoded))
	require.True(t, data.SystemCounter("trial_blocked"))
	require.False(t, data.SystemCounter("trials"))
	repo, _ := newLoyaltyRepo(t)
	_, err := repo.CounterCreate(t.Context(), 42, "trial_blocked", "")
	require.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput, "a streamer must not create or set a trial counter")
}

type entryProbe struct {
	viewerID uint64
	command  string
	want     int64
}

func TestCounterSetTargetsOneEntryPerScope(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scope  string
		seed   *loyaltyrepo.SetTarget
		target loyaltyrepo.SetTarget
		probes []entryProbe
	}{
		{
			name:   "command scope writes the normalized command bucket",
			scope:  data.CounterScopeCommand,
			target: loyaltyrepo.SetTarget{Command: "!Raid"},
			probes: []entryProbe{{0, "raid", 5}, {0, "hug", 0}},
		},
		{
			name:   "viewer scope writes only the targeted viewer",
			scope:  data.CounterScopeViewer,
			target: loyaltyrepo.SetTarget{ViewerID: 7},
			probes: []entryProbe{{7, "", 5}, {8, "", 0}},
		},
		{
			name:   "viewer command scope writes one viewer and command",
			scope:  data.CounterScopeViewerCommand,
			target: loyaltyrepo.SetTarget{ViewerID: 7, Command: "!Hug"},
			probes: []entryProbe{{7, "hug", 5}, {7, "bonk", 0}, {8, "hug", 0}},
		},
		{
			name:   "an untargeted set clears every entry",
			scope:  data.CounterScopeViewer,
			seed:   &loyaltyrepo.SetTarget{ViewerID: 7},
			probes: []entryProbe{{7, "", 0}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _ := newLoyaltyRepo(t)
			ctx := t.Context()
			_, err := repo.CounterCreate(ctx, 1, "tally", tc.scope)
			require.NoError(t, err)
			if tc.seed != nil {
				_, err = repo.CounterSet(ctx, 1, "tally", *tc.seed, 3)
				require.NoError(t, err)
			}
			found, err := repo.CounterSet(ctx, 1, "tally", tc.target, 5)
			require.NoError(t, err)
			require.True(t, found)
			for _, probe := range tc.probes {
				_, got, found, err := repo.CounterGet(ctx, 1, "tally", probe.viewerID, probe.command)
				require.NoError(t, err)
				require.True(t, found)
				assert.Equal(t, probe.want, got, "viewer=%d command=%q", probe.viewerID, probe.command)
			}
		})
	}
}

func TestCounterEntryDeleteRefusesUntargetedCall(t *testing.T) {
	repo, _ := newLoyaltyRepo(t)
	ctx := t.Context()
	_, err := repo.CounterCreate(ctx, 1, "tally", data.CounterScopeViewer)
	require.NoError(t, err)
	_, err = repo.CounterSet(ctx, 1, "tally", loyaltyrepo.SetTarget{ViewerID: 7}, 5)
	require.NoError(t, err)

	deleted, err := repo.CounterEntryDelete(ctx, 1, "tally", loyaltyrepo.SetTarget{})
	require.NoError(t, err)
	assert.False(t, deleted)
	_, got, _, err := repo.CounterGet(ctx, 1, "tally", 7, "")
	require.NoError(t, err)
	assert.EqualValues(t, 5, got, "an untargeted delete must keep every entry")

	deleted, err = repo.CounterEntryDelete(ctx, 1, "tally", loyaltyrepo.SetTarget{ViewerID: 7})
	require.NoError(t, err)
	assert.True(t, deleted)
	_, got, _, err = repo.CounterGet(ctx, 1, "tally", 7, "")
	require.NoError(t, err)
	assert.Zero(t, got)
}
