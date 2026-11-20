// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/ent/voiceusage"
	"ItsBagelBot/app/db/users/repository"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const voiceOwnerID = 1001

func newVoiceAccount(t *testing.T) (*ent.Client, *repository.Users) {
	t.Helper()
	client, _, repo := setup(t)
	require.NoError(t, repo.Register(t.Context(), repository.Registration{
		ID: voiceOwnerID, Username: "voiceowner", Email: "voiceowner@example.invalid",
	}))
	return client, repo
}

func voiceAllowanceAt(t *testing.T, repo *repository.Users, at time.Time) usersrpc.VoiceAllowance {
	t.Helper()
	allowance, err := repo.VoiceAllowance(t.Context(), voiceOwnerID, at)
	require.NoError(t, err)
	return allowance
}

func recordVoiceAt(t *testing.T, repo *repository.Users, record repository.VoiceUsageRecord, at time.Time) usersrpc.VoiceAllowance {
	t.Helper()
	allowance, err := repo.RecordVoiceUsage(t.Context(), voiceOwnerID, record, at)
	require.NoError(t, err)
	return allowance
}

func TestVoiceAllowancePlans(t *testing.T) {
	client, repo := newVoiceAccount(t)
	at := time.Date(2026, 11, 30, 20, 0, 0, 0, time.FixedZone("UTC-5", -5*3600))
	cases := []struct {
		name   string
		status user.Status
		source string
		active bool
		plan   voiceplan.Plan
	}{
		{"purchase", user.StatusPaid, "tebex", true, voiceplan.Premium},
		{"giveaway", user.StatusPaid, "giveaway", true, voiceplan.Giveaway},
		{"admin grant", user.StatusPaid, "admin", true, voiceplan.AdminGrant},
		{"vip", user.StatusVip, "", true, voiceplan.VIP},
		{"free", user.StatusFree, "", true, voiceplan.Free},
		{"inactive", user.StatusPaid, "tebex", false, voiceplan.Free},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client.User.UpdateOneID(voiceOwnerID).
				SetStatus(tc.status).SetSubscriptionSource(tc.source).SetIsActive(tc.active).ExecX(t.Context())
			limit := voiceplan.SecondsToTokens(voiceplan.MonthlySeconds(tc.plan))
			assert.Equal(t, usersrpc.VoiceAllowance{
				Plan: tc.plan, Period: "2026-12", LimitTokens: limit, RemainingTokens: limit,
				ResetsAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
				Enabled:  true, Voice: voiceplan.DefaultVoice,
			}, voiceAllowanceAt(t, repo, at))
		})
	}
}

func TestVoiceUsageAccumulatesAndResets(t *testing.T) {
	client, repo := newVoiceAccount(t)
	client.User.UpdateOneID(voiceOwnerID).SetStatus(user.StatusVip).ExecX(t.Context())
	monthEnd := time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC)
	newMonth := monthEnd.Add(time.Minute)
	first := repository.VoiceUsageRecord{RequestID: "first", AudioTokens: 100, DurationMS: 4000}
	start := recordVoiceAt(t, repo, first, monthEnd)
	second := repository.VoiceUsageRecord{RequestID: "second", AudioTokens: start.LimitTokens, DurationMS: 2000}
	exhausted := recordVoiceAt(t, repo, second, monthEnd)

	assert.Equal(t, start.LimitTokens+first.AudioTokens, exhausted.UsedTokens)
	assert.Zero(t, exhausted.RemainingTokens)
	assert.Equal(t, exhausted, voiceAllowanceAt(t, repo, monthEnd))

	require.NoError(t, repo.SetVoiceSettings(t.Context(), voiceOwnerID, false, "Puck"))
	assert.Equal(t, start, recordVoiceAt(t, repo, first, newMonth))
	assert.Zero(t, voiceAllowanceAt(t, repo, newMonth).UsedTokens)

	third := repository.VoiceUsageRecord{RequestID: "third", AudioTokens: 25, DurationMS: 1000}
	reset := recordVoiceAt(t, repo, third, newMonth)
	assert.Equal(t, "2027-01", reset.Period)
	assert.Equal(t, third.AudioTokens, reset.UsedTokens)
	assertVoiceLedgerTotals(t, client, [][3]int64{
		{exhausted.UsedTokens, first.DurationMS + second.DurationMS, 2},
		{third.AudioTokens, third.DurationMS, 1},
	})
}

func assertVoiceLedgerTotals(t *testing.T, client *ent.Client, want [][3]int64) {
	t.Helper()
	rows := client.VoiceUsage.Query().Order(ent.Asc(voiceusage.FieldPeriod)).AllX(t.Context())
	got := make([][3]int64, 0, len(rows))
	for _, row := range rows {
		got = append(got, [3]int64{row.AudioTokens, row.DurationMs, int64(row.Requests)})
	}
	assert.Equal(t, want, got)
}

func TestVoiceUsageRejectsInvalidInput(t *testing.T) {
	client, repo := newVoiceAccount(t)
	cases := map[string]repository.VoiceUsageRecord{
		"negative tokens":   {RequestID: "tokens", AudioTokens: -1},
		"negative duration": {RequestID: "duration", DurationMS: -1},
		"missing ID":        {},
		"long ID":           {RequestID: strings.Repeat("x", usersrpc.VoiceRequestIDMaxBytes+1)},
	}
	for name, record := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := repo.RecordVoiceUsage(t.Context(), voiceOwnerID, record, time.Now())
			assert.ErrorIs(t, err, repository.ErrInvalidVoiceUsage)
		})
	}
	assert.Zero(t, client.VoiceUsage.Query().CountX(t.Context()))
	assert.Zero(t, client.VoiceUsageReceipt.Query().CountX(t.Context()))
}

func TestVoiceSettingsValidation(t *testing.T) {
	_, repo := newVoiceAccount(t)
	cases := []struct {
		name, voice string
		enabled     bool
		wantErr     error
		wantVoice   string
		wantEnabled bool
	}{
		{"prebuilt voice", "Zubenelgenubi", false, nil, "Zubenelgenubi", false},
		{"wrong case", "kore", true, repository.ErrInvalidVoice, "Zubenelgenubi", false},
		{"unknown voice", "Desktop", true, repository.ErrInvalidVoice, "Zubenelgenubi", false},
		{"reset to default", "", true, nil, voiceplan.DefaultVoice, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := repo.SetVoiceSettings(t.Context(), voiceOwnerID, tc.enabled, tc.voice)
			require.ErrorIs(t, err, tc.wantErr)
			allowance := voiceAllowanceAt(t, repo, time.Now())
			assert.Equal(t, tc.wantVoice, allowance.Voice)
			assert.Equal(t, tc.wantEnabled, allowance.Enabled)
		})
	}
}

func TestVoiceUsageErasure(t *testing.T) {
	client, repo := newVoiceAccount(t)
	const otherID = 1002
	ctx := t.Context()
	client.User.Create().SetID(otherID).SetUsername("otherowner").SetEmail("other@example.invalid").ExecX(ctx)
	now := time.Now()
	record := repository.VoiceUsageRecord{RequestID: "shared", AudioTokens: 25}
	recordVoiceAt(t, repo, record, now)
	_, err := repo.RecordVoiceUsage(ctx, otherID, record, now)
	require.NoError(t, err)

	require.NoError(t, repo.Delete(t.Context(), voiceOwnerID))
	assert.Equal(t, uint64(otherID), client.VoiceUsage.Query().OnlyX(ctx).UserID)
	assert.Equal(t, uint64(otherID), client.VoiceUsageReceipt.Query().OnlyX(ctx).UserID)
}

func TestVoiceReceiptRetention(t *testing.T) {
	client, repo := newVoiceAccount(t)
	now := time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -40)
	recordVoiceAt(t, repo, repository.VoiceUsageRecord{RequestID: "expired", AudioTokens: 25}, cutoff)
	recordVoiceAt(t, repo, repository.VoiceUsageRecord{RequestID: "retained", AudioTokens: 50}, cutoff.Add(time.Second))
	deleted, err := repo.CleanupVoiceUsageReceipts(t.Context(), now)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)
	assert.Equal(t, "retained", client.VoiceUsageReceipt.Query().OnlyX(t.Context()).RequestID)

	assert.Equal(t, int64(75), voiceAllowanceAt(t, repo, cutoff).UsedTokens)
}
