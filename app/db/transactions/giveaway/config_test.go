package giveaway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfigGatesKeepDrawRecoverySeparate(t *testing.T) {
	c := Config{NewAwardsEnabled: true}
	require.True(t, c.CanCreateNewAwards())
	require.False(t, c.CanScheduleAwards())
	require.False(t, c.CanMutateProvider())
	c.PromotionalGrantsEnabled = true
	require.True(t, c.CanSchedulePromotionalGrants())
	c.IntervalRuleVerified = true
	require.True(t, c.CanScheduleAwards())
	require.False(t, c.CanMutateProvider())
	c.NewAwardsEnabled = false
	require.True(t, c.CanScheduleAwards())
	c.ProviderMutations = true
	require.True(t, c.CanMutateProvider())
}

func TestConfigDefaultsRenewalBuffer(t *testing.T) {
	t.Setenv(EnvRenewalBuffer, "")
	if got := ConfigFromEnv().RenewalBuffer; got != 72*time.Hour {
		t.Fatalf("renewal buffer = %s, want 72h", got)
	}
}

func TestConfigPromotionalGrantsRequireExplicitEnable(t *testing.T) {
	t.Setenv(EnvPromotionalGrants, "")
	if ConfigFromEnv().PromotionalGrantsEnabled {
		t.Fatal("promotional grants should remain off until explicitly enabled")
	}
	t.Setenv(EnvPromotionalGrants, "true")
	if !ConfigFromEnv().PromotionalGrantsEnabled {
		t.Fatal("promotional grant gate should honor an explicit enable")
	}
	t.Setenv(EnvPromotionalGrants, "invalid")
	if ConfigFromEnv().PromotionalGrantsEnabled {
		t.Fatal("malformed promotional grant gate should fail closed")
	}
}
