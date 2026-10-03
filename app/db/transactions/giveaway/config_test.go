// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package giveaway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConfigFromEnv(t *testing.T) {
	defaults := Config{ReconcileInterval: 15 * time.Minute, BoundaryInterval: time.Minute, RenewalBuffer: 72 * time.Hour}
	enabled := func(edit func(*Config)) Config {
		cfg := defaults
		edit(&cfg)
		return cfg
	}
	for _, tc := range []struct {
		name string
		env  map[string]string
		want Config
	}{
		{name: "keeps every launch gate closed by default", want: defaults},
		{
			name: "TestConfigPromotionalGrantsRequireExplicitEnable honors an explicit enable",
			env:  map[string]string{EnvPromotionalGrants: "true"},
			want: enabled(func(c *Config) { c.PromotionalGrantsEnabled = true }),
		},
		{
			name: "TestConfigPromotionalGrantsRequireExplicitEnable fails closed on a malformed value",
			env:  map[string]string{EnvPromotionalGrants: "invalid"},
			want: defaults,
		},
		{
			name: "reads each gate and interval from its own variable",
			env: map[string]string{
				EnvNewAwardsEnabled: "true", EnvIntervalRuleVerified: "true", EnvProviderMutations: "true",
				EnvReconcileInterval: "5m", EnvBoundaryInterval: "30s", EnvRenewalBuffer: "24h",
			},
			want: Config{NewAwardsEnabled: true, IntervalRuleVerified: true, ProviderMutations: true, ReconcileInterval: 5 * time.Minute, BoundaryInterval: 30 * time.Second, RenewalBuffer: 24 * time.Hour},
		},
		{
			name: "falls back to the defaults for unusable durations",
			env:  map[string]string{EnvReconcileInterval: "-1m", EnvBoundaryInterval: "soon", EnvRenewalBuffer: "0s"},
			want: defaults,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{EnvNewAwardsEnabled, EnvPromotionalGrants, EnvIntervalRuleVerified, EnvProviderMutations, EnvReconcileInterval, EnvBoundaryInterval, EnvRenewalBuffer} {
				t.Setenv(key, tc.env[key])
			}
			assert.Equal(t, tc.want, ConfigFromEnv())
		})
	}
}

func TestConfigGatesKeepDrawRecoverySeparate(t *testing.T) {
	type gates struct{ create, schedule, promotional, mutateProvider bool }
	for _, tc := range []struct {
		name string
		cfg  Config
		want gates
	}{
		{"new awards alone only allow creating awards", Config{NewAwardsEnabled: true}, gates{create: true}},
		{"promotional grants schedule without a verified provider rule", Config{NewAwardsEnabled: true, PromotionalGrantsEnabled: true}, gates{create: true, promotional: true}},
		{"a verified rule schedules awards but does not mutate the provider", Config{NewAwardsEnabled: true, PromotionalGrantsEnabled: true, IntervalRuleVerified: true}, gates{create: true, schedule: true, promotional: true}},
		{"disabling new awards keeps recovery scheduling", Config{PromotionalGrantsEnabled: true, IntervalRuleVerified: true}, gates{schedule: true, promotional: true}},
		{"provider mutations need both the gate and a verified rule", Config{IntervalRuleVerified: true, ProviderMutations: true}, gates{schedule: true, mutateProvider: true}},
		{"provider mutations stay off without a verified rule", Config{ProviderMutations: true}, gates{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := gates{tc.cfg.CanCreateNewAwards(), tc.cfg.CanScheduleAwards(), tc.cfg.CanSchedulePromotionalGrants(), tc.cfg.CanMutateProvider()}
			assert.Equal(t, tc.want, got)
		})
	}
}
