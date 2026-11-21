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
		active, banned       bool
		plan                 voiceplan.Plan
		minutes              int64
	}{
		{"free", "free", "", true, false, voiceplan.Free, 0},
		{"purchase", "paid", "tebex", true, false, voiceplan.Premium, 60},
		{"giveaway", "paid", "giveaway", true, false, voiceplan.Giveaway, 60},
		{"admin grant", "paid", "admin", true, false, voiceplan.AdminGrant, 30},
		{"vip takes precedence", "vip", "tebex", true, false, voiceplan.VIP, 120},
		{"inactive purchase", "paid", "tebex", false, false, voiceplan.Free, 0},
		{"inactive giveaway", "paid", "giveaway", false, false, voiceplan.Free, 0},
		{"inactive admin grant", "paid", "admin", false, false, voiceplan.Free, 0},
		{"inactive vip", "vip", "", false, false, voiceplan.Free, 0},
		{"banned purchase", "paid", "tebex", true, true, voiceplan.Free, 0},
		{"banned giveaway", "paid", "giveaway", true, true, voiceplan.Free, 0},
		{"banned admin grant", "paid", "admin", true, true, voiceplan.Free, 0},
		{"banned vip", "vip", "", true, true, voiceplan.Free, 0},
		{"missing source", "paid", "", true, false, voiceplan.Free, 0},
		{"unknown source", "paid", "other", true, false, voiceplan.Free, 0},
		{"free with source", "free", "admin", true, false, voiceplan.Free, 0},
		{"unknown status", "other", "tebex", true, false, voiceplan.Free, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := voiceplan.PlanFor(tc.status, tc.source, tc.active, tc.banned)
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
