// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package voiceplan_test

import (
	"testing"
	"time"

	"ItsBagelBot/internal/domain/voiceplan"
	"github.com/stretchr/testify/assert"
)

func TestMonthlyAllowance(t *testing.T) {
	tests := []struct {
		name, status, source string
		active               bool
		plan                 voiceplan.Plan
		minutes              int64
	}{
		{"free", "free", "", true, voiceplan.Free, 0},
		{"purchase", "paid", "tebex", true, voiceplan.Premium, 60},
		{"giveaway", "paid", "giveaway", true, voiceplan.Giveaway, 60},
		{"admin grant", "paid", "admin", true, voiceplan.AdminGrant, 30},
		{"vip takes precedence", "vip", "tebex", true, voiceplan.VIP, 120},
		{"inactive purchase", "paid", "tebex", false, voiceplan.Free, 0},
		{"inactive giveaway", "paid", "giveaway", false, voiceplan.Free, 0},
		{"inactive admin grant", "paid", "admin", false, voiceplan.Free, 0},
		{"inactive vip", "vip", "", false, voiceplan.Free, 0},
		{"missing source", "paid", "", true, voiceplan.Free, 0},
		{"unknown source", "paid", "other", true, voiceplan.Free, 0},
		{"free with source", "free", "admin", true, voiceplan.Free, 0},
		{"unknown status", "other", "tebex", true, voiceplan.Free, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := voiceplan.PlanFor(tc.status, tc.source, tc.active)
			assert.Equal(t, tc.plan, plan)
			assert.Equal(t, tc.minutes*60, voiceplan.MonthlySeconds(plan))
		})
	}
}

func TestTokenConversions(t *testing.T) {
	assert.Equal(t, 25, voiceplan.TokensPerSecond)
	tests := []struct {
		name            string
		seconds, tokens int64
	}{
		{"zero", 0, 0},
		{"one second", 1, 25},
		{"one minute", 60, 1500},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.tokens, voiceplan.SecondsToTokens(tc.seconds))
			assert.Equal(t, tc.seconds, voiceplan.TokensToSeconds(tc.tokens))
			assert.Equal(t, tc.seconds, voiceplan.TokensToSeconds(tc.tokens+24))
		})
	}
}

func TestValidVoice(t *testing.T) {
	assert.Equal(t, "Kore", voiceplan.DefaultVoice)
	tests := []struct {
		voice string
		valid bool
	}{
		{voiceplan.DefaultVoice, true},
		{"Zephyr", true},
		{"Zubenelgenubi", true},
		{"Sulafat", true},
		{"kore", false},
		{"Kore ", false},
		{"Desktop", false},
		{"", false},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.valid, voiceplan.ValidVoice(tc.voice), "voice %q", tc.voice)
	}
}

func TestMonthlyPeriod(t *testing.T) {
	tests := []struct {
		name, period string
		at, reset    time.Time
	}{
		{"local month lags UTC", "2026-12", time.Date(2026, 11, 30, 23, 0, 0, 0, time.FixedZone("UTC-5", -5*3600)), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"local month leads UTC", "2024-02", time.Date(2024, 3, 1, 0, 0, 0, 0, time.FixedZone("UTC+2", 2*3600)), time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
		{"at UTC reset", "2026-01", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.period, voiceplan.Period(tc.at))
			assert.Equal(t, tc.reset, voiceplan.ResetsAt(tc.at))
		})
	}
}
