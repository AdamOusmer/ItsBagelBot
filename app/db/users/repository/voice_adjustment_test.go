// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/voiceplan"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdjustVoiceUsageCreatesUTCPeriodAtPlanPercentage(t *testing.T) {
	at := time.Date(2026, 11, 30, 20, 0, 0, 0, time.FixedZone("UTC-5", -5*3600))
	for _, tc := range []struct {
		plan    voiceplan.Plan
		status  user.Status
		percent int
	}{
		{voiceplan.Premium, user.StatusPaid, 25},
		{voiceplan.VIP, user.StatusVip, 75},
		{voiceplan.Premium, user.StatusPaid, 0},
		{voiceplan.VIP, user.StatusVip, 100},
	} {
		t.Run(string(tc.plan), func(t *testing.T) {
			client, repo := newVoiceAccount(t)
			client.User.UpdateOneID(voiceOwnerID).SetIsActive(true).
				SetStatus(tc.status).SetSubscriptionSource("tebex").ExecX(t.Context())

			change := repository.VoiceUsageChange{UsedPercent: &tc.percent}
			got, err := repo.AdjustVoiceUsage(t.Context(), voiceOwnerID, change, at)
			require.NoError(t, err)

			limit := voiceplan.SecondsToTokens(voiceplan.MonthlySeconds(tc.plan))
			used := limit * int64(tc.percent) / 100
			assert.Equal(t, []int64{limit, used, limit - used}, []int64{got.LimitTokens, got.UsedTokens, got.RemainingTokens})
			assert.Equal(t, []any{voiceAllowanceAt(t, repo, at), "2026-12"}, []any{got, got.Period})
			assertVoiceLedgerTotals(t, client, [][3]int64{{used, 0, 0}})
		})
	}
}

func TestAdjustVoiceUsageResetPreservesRequestsAndReceipts(t *testing.T) {
	client, repo := newVoiceAccount(t)
	client.User.UpdateOneID(voiceOwnerID).SetIsActive(true).SetStatus(user.StatusVip).ExecX(t.Context())
	at := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	record := repository.VoiceUsageRecord{RequestID: "current", AudioTokens: 200, DurationMS: 8000}
	previous := repository.VoiceUsageRecord{RequestID: "previous", AudioTokens: 100, DurationMS: 4000}
	recordVoiceAt(t, repo, previous, at.Add(-time.Second))
	receipt := recordVoiceAt(t, repo, record, at)

	got, err := repo.AdjustVoiceUsage(t.Context(), voiceOwnerID, repository.VoiceUsageChange{Reset: true}, at)
	require.NoError(t, err)

	assert.Equal(t, []int64{0, got.LimitTokens}, []int64{got.UsedTokens, got.RemainingTokens})
	assert.Equal(t, receipt, recordVoiceAt(t, repo, record, at))
	assertVoiceLedgerTotals(t, client, [][3]int64{{previous.AudioTokens, previous.DurationMS, 1}, {0, record.DurationMS, 1}})
}

func TestAdjustVoiceUsageRefusesInvalidChanges(t *testing.T) {
	negative, excessive := -1, 101
	for _, tc := range []struct {
		name   string
		status user.Status
		change repository.VoiceUsageChange
		err    error
	}{
		{"no allowance", user.StatusFree, repository.VoiceUsageChange{Reset: true}, repository.ErrNoVoiceAllowance},
		{"negative", user.StatusVip, repository.VoiceUsageChange{UsedPercent: &negative}, repository.ErrInvalidVoiceUsageChange},
		{"above 100", user.StatusVip, repository.VoiceUsageChange{UsedPercent: &excessive}, repository.ErrInvalidVoiceUsageChange},
		{"missing", user.StatusVip, repository.VoiceUsageChange{}, repository.ErrInvalidVoiceUsageChange},
		{"both", user.StatusVip, repository.VoiceUsageChange{Reset: true, UsedPercent: &negative}, repository.ErrInvalidVoiceUsageChange},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, repo := newVoiceAccount(t)
			client.User.UpdateOneID(voiceOwnerID).SetIsActive(true).SetStatus(tc.status).ExecX(t.Context())

			_, err := repo.AdjustVoiceUsage(t.Context(), voiceOwnerID, tc.change, time.Now())
			assert.ErrorIs(t, err, tc.err)
			assert.Zero(t, client.VoiceUsage.Query().CountX(t.Context()))
		})
	}
}
