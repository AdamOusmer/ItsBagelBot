package giveaway

import (
	"time"

	"ItsBagelBot/pkg/env"
)

const (
	EnvNewAwardsEnabled     = "GIVEAWAYS_NEW_AWARDS_ENABLED"
	EnvPromotionalGrants    = "GIVEAWAYS_PROMOTIONAL_GRANTS_ENABLED"
	EnvIntervalRuleVerified = "GIVEAWAYS_INTERVAL_RULE_VERIFIED"
	EnvProviderMutations    = "TEBEX_CHECKOUT_MUTATIONS_ENABLED"
	EnvReconcileInterval    = "GIVEAWAYS_RECONCILE_INTERVAL"
	EnvBoundaryInterval     = "GIVEAWAYS_BOUNDARY_RECONCILE_INTERVAL"
	EnvRenewalBuffer        = "GIVEAWAYS_RENEWAL_BUFFER"
)

type Config struct {
	NewAwardsEnabled         bool
	PromotionalGrantsEnabled bool
	IntervalRuleVerified     bool
	ProviderMutations        bool
	ReconcileInterval        time.Duration
	BoundaryInterval         time.Duration
	RenewalBuffer            time.Duration
}

func ConfigFromEnv() Config {
	return Config{
		NewAwardsEnabled:         envBool(EnvNewAwardsEnabled),
		PromotionalGrantsEnabled: envBool(EnvPromotionalGrants),
		IntervalRuleVerified:     envBool(EnvIntervalRuleVerified),
		ProviderMutations:        envBool(EnvProviderMutations),
		ReconcileInterval:        envDuration(EnvReconcileInterval, 15*time.Minute),
		BoundaryInterval:         envDuration(EnvBoundaryInterval, time.Minute),
		RenewalBuffer:            envDuration(EnvRenewalBuffer, 72*time.Hour),
	}
}

func envBool(key string) bool { return env.GetBool(key, false) }

func envDuration(key string, fallback time.Duration) time.Duration {
	if value := env.GetDuration(key, fallback); value > 0 {
		return value
	}
	return fallback
}

func (c Config) CanCreateNewAwards() bool { return c.NewAwardsEnabled }
func (c Config) CanScheduleAwards() bool  { return c.IntervalRuleVerified }
func (c Config) CanSchedulePromotionalGrants() bool {
	return c.PromotionalGrantsEnabled
}
func (c Config) CanMutateProvider() bool { return c.ProviderMutations && c.IntervalRuleVerified }
