package giveaway

import (
	"testing"
	"time"
)

func TestConfigGatesKeepDrawRecoverySeparate(t *testing.T) {
	c := Config{NewAwardsEnabled: true}
	if !c.CanCreateNewAwards() {
		t.Fatal("new award gate should permit durable selection")
	}
	if c.CanScheduleAwards() || c.CanMutateProvider() {
		t.Fatal("unverified interval must block fulfillment and provider mutation")
	}
	c.IntervalRuleVerified = true
	if !c.CanScheduleAwards() || c.CanMutateProvider() {
		t.Fatal("provider gate must remain closed until explicitly enabled")
	}
	c.NewAwardsEnabled = false
	if !c.CanScheduleAwards() {
		t.Fatal("disabling new draws must not stop recovery of existing awards")
	}
	c.ProviderMutations = true
	if !c.CanMutateProvider() {
		t.Fatal("explicit provider gate should enable mutation")
	}
}

func TestConfigDefaultsRenewalBuffer(t *testing.T) {
	t.Setenv(EnvRenewalBuffer, "")
	if got := ConfigFromEnv().RenewalBuffer; got != 72*time.Hour {
		t.Fatalf("renewal buffer = %s, want 72h", got)
	}
}
