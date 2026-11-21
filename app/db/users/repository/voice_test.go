// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/premiumgrant"
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
		name           string
		status         user.Status
		source         string
		active, banned bool
		plan           voiceplan.Plan
		reason         string
	}{
		{"purchase", user.StatusPaid, "tebex", true, false, voiceplan.Premium, "ok"},
		{"giveaway", user.StatusPaid, "giveaway", true, false, voiceplan.Giveaway, "ok"},
		{"admin grant", user.StatusPaid, "admin", true, false, voiceplan.AdminGrant, "ok"},
		{"vip", user.StatusVip, "", true, false, voiceplan.VIP, "ok"},
		{"free", user.StatusFree, "", true, false, voiceplan.Free, "no_plan"},
		{"inactive", user.StatusPaid, "tebex", false, false, voiceplan.Free, "inactive"},
		{"banned", user.StatusPaid, "tebex", true, true, voiceplan.Free, "banned"},
		{"inactive and banned", user.StatusVip, "", false, true, voiceplan.Free, "banned"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client.User.UpdateOneID(voiceOwnerID).
				SetStatus(tc.status).SetSubscriptionSource(tc.source).SetIsActive(tc.active).SetBanned(tc.banned).ExecX(t.Context())
			limit := voiceplan.SecondsToTokens(voiceplan.MonthlySeconds(tc.plan))
			assert.Equal(t, usersrpc.VoiceAllowance{
				Plan: tc.plan, Period: "2026-12", LimitTokens: limit, RemainingTokens: limit,
				ResetsAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
				Reason:   tc.reason,
			}, voiceAllowanceAt(t, repo, at))
		})
	}
}

func TestVoiceAllowanceAdminGrantOverlap(t *testing.T) {
	client, repo := newVoiceAccount(t)
	ctx := t.Context()
	client.User.UpdateOneID(voiceOwnerID).SetStatus(user.StatusPaid).SetSubscriptionSource("admin").ExecX(ctx)
	start := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	client.PremiumGrant.Create().SetUserID(voiceOwnerID).SetGiveawayID("giveaway").SetAwardID("committed").
		SetIntervalRuleVersion("test").SetState(premiumgrant.StateCommitted).SetStartAt(start).SetEndAt(end).ExecX(ctx)
	client.PremiumGrant.Create().SetUserID(voiceOwnerID).SetGiveawayID("giveaway").SetAwardID("prepared").
		SetIntervalRuleVersion("test").SetState(premiumgrant.StatePrepared).SetStartAt(start.Add(-time.Hour)).SetEndAt(start).ExecX(ctx)

	cases := []struct {
		name string
		at   time.Time
		plan voiceplan.Plan
	}{
		{"prepared interval with a future committed grant", start.Add(-time.Microsecond), voiceplan.AdminGrant},
		{"first instant of committed interval", start, voiceplan.Giveaway},
		{"last instant of committed interval", end.Add(-time.Microsecond), voiceplan.Giveaway},
		{"end of committed interval", end, voiceplan.AdminGrant},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := voiceAllowanceAt(t, repo, tc.at)
			assert.Equal(t, tc.plan, got.Plan)
			assert.Equal(t, voiceplan.SecondsToTokens(voiceplan.MonthlySeconds(tc.plan)), got.LimitTokens)
		})
	}
}

func TestVoiceUsageAccumulatesAndResets(t *testing.T) {
	client, repo := newVoiceAccount(t)
	client.User.UpdateOneID(voiceOwnerID).SetStatus(user.StatusPaid).SetSubscriptionSource("admin").ExecX(t.Context())
	monthEnd := time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC)
	newMonth := monthEnd.Add(time.Minute)
	first := repository.VoiceUsageRecord{RequestID: "first", AudioTokens: 100, DurationMS: 4000}
	start := recordVoiceAt(t, repo, first, monthEnd)
	batch := repository.VoiceUsageRecord{
		AudioTokens: voiceplan.SecondsToTokens(voiceplan.MaxClipSeconds),
		DurationMS:  (voiceplan.MaxClipSeconds * time.Second).Milliseconds(),
	}
	fullClips := start.LimitTokens / batch.AudioTokens
	exhausted := start
	for i := range fullClips {
		batch.RequestID = strconv.FormatInt(i, 10)
		exhausted = recordVoiceAt(t, repo, batch, monthEnd)
	}
	wantExhausted := start
	wantExhausted.UsedTokens = first.AudioTokens + fullClips*batch.AudioTokens
	wantExhausted.RemainingTokens = 0
	assert.Equal(t, wantExhausted, exhausted)
	assert.Equal(t, exhausted, voiceAllowanceAt(t, repo, monthEnd))

	assert.Equal(t, start, recordVoiceAt(t, repo, first, newMonth))
	assert.Zero(t, voiceAllowanceAt(t, repo, newMonth).UsedTokens)

	next := repository.VoiceUsageRecord{RequestID: "next", AudioTokens: 25, DurationMS: 1000}
	reset := recordVoiceAt(t, repo, next, newMonth)
	assert.Equal(t, "2027-01", reset.Period)
	assert.Equal(t, next.AudioTokens, reset.UsedTokens)
	assertVoiceLedgerTotals(t, client, [][3]int64{
		{wantExhausted.UsedTokens, first.DurationMS + fullClips*batch.DurationMS, fullClips + 1},
		{next.AudioTokens, next.DurationMS, 1},
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
		"excess tokens":     {RequestID: "tokens", AudioTokens: voiceplan.SecondsToTokens(voiceplan.MaxClipSeconds) + 1},
		"excess duration":   {RequestID: "duration", DurationMS: (voiceplan.MaxClipSeconds * time.Second).Milliseconds() + 1},
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
	expired := repository.VoiceUsageRecord{RequestID: "expired", AudioTokens: 25}
	recordVoiceAt(t, repo, expired, cutoff)
	recordVoiceAt(t, repo, repository.VoiceUsageRecord{RequestID: "retained", AudioTokens: 50}, cutoff.Add(time.Second))
	deleted, err := repo.CleanupVoiceUsageReceipts(t.Context(), now)
	require.NoError(t, err)
	type cleanupResult struct {
		deleted  int
		retained string
	}
	assert.Equal(t, cleanupResult{1, "retained"},
		cleanupResult{deleted, client.VoiceUsageReceipt.Query().OnlyX(t.Context()).RequestID})

	assert.Equal(t, int64(75), voiceAllowanceAt(t, repo, cutoff).UsedTokens)
	assert.Equal(t, expired.AudioTokens, recordVoiceAt(t, repo, expired, now).UsedTokens)
}
