package giveaway

import (
	"os"
	"strconv"
	"time"
)

const (
	EnvNewAwardsEnabled     = "GIVEAWAYS_NEW_AWARDS_ENABLED"
	EnvIntervalRuleVerified = "GIVEAWAYS_INTERVAL_RULE_VERIFIED"
	EnvProviderMutations    = "TEBEX_CHECKOUT_MUTATIONS_ENABLED"
	EnvReconcileInterval    = "GIVEAWAYS_RECONCILE_INTERVAL"
	EnvBoundaryInterval     = "GIVEAWAYS_BOUNDARY_RECONCILE_INTERVAL"
	EnvRenewalBuffer        = "GIVEAWAYS_RENEWAL_BUFFER"
)

// Config keeps launch gates independent: disabling new awards does not stop
// recovery and monitoring for already-selected obligations.
type Config struct {
	NewAwardsEnabled     bool
	IntervalRuleVerified bool
	ProviderMutations    bool
	ReconcileInterval    time.Duration
	BoundaryInterval     time.Duration
	RenewalBuffer        time.Duration
}

func ConfigFromEnv() Config {
	return Config{
		NewAwardsEnabled:     envBool(EnvNewAwardsEnabled),
		IntervalRuleVerified: envBool(EnvIntervalRuleVerified),
		ProviderMutations:    envBool(EnvProviderMutations),
		ReconcileInterval:    envDuration(EnvReconcileInterval, 15*time.Minute),
		BoundaryInterval:     envDuration(EnvBoundaryInterval, time.Minute),
		RenewalBuffer:        envDuration(EnvRenewalBuffer, 72*time.Hour),
	}
}

func envBool(key string) bool { value, _ := strconv.ParseBool(os.Getenv(key)); return value }
func envDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(os.Getenv(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

// Draws remain available while provider semantics are being verified: the
// winner obligation and selection notice are durable and can stay pending.
// Only scheduling/committing a Premium interval and provider mutation need
// the verified interval rule.
func (c Config) CanCreateNewAwards() bool { return c.NewAwardsEnabled }
func (c Config) CanScheduleAwards() bool  { return c.IntervalRuleVerified }
func (c Config) CanMutateProvider() bool  { return c.ProviderMutations && c.IntervalRuleVerified }
