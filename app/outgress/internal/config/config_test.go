package config

import "testing"

func TestLoadRateRegion(t *testing.T) {
	t.Setenv("TWITCH_CLIENT_ID", "test-client")
	t.Setenv("TWITCH_CLIENT_SECRET", "test-secret")

	t.Run("safe fallback", func(t *testing.T) {
		t.Setenv("OUTGRESS_REGION", "")
		if got := Load().RateRegion; got != "local" {
			t.Fatalf("RateRegion = %q, want local", got)
		}
	})

	t.Run("explicit locality", func(t *testing.T) {
		t.Setenv("OUTGRESS_REGION", "node2")
		if got := Load().RateRegion; got != "node2" {
			t.Fatalf("RateRegion = %q, want node2", got)
		}
	})
}
