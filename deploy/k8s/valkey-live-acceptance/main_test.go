package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidatesWriteProfile(t *testing.T) {
	for _, profile := range []string{"pooled", "pipeline"} {
		t.Run(profile, func(t *testing.T) {
			cfg := validTestConfig()
			cfg.writeProfile = profile
			require.NoError(t, cfg.validate())
		})
	}

	cfg := validTestConfig()
	cfg.writeProfile = "unbounded-connections"
	require.EqualError(t, cfg.validate(), "write-profile must be pooled or pipeline")
}

func TestSummarizeReportsProfileAndFullLatencyRange(t *testing.T) {
	cfg := validTestConfig()
	cfg.writeProfile = "pipeline"
	measured := measurement{
		latencies: []time.Duration{4 * time.Millisecond, time.Millisecond, 2 * time.Millisecond},
		elapsed:   time.Second,
	}

	report := summarize(cfg, operation{name: "write-master"}, measured)

	assert.Equal(t, "pipeline", report.WriteProfile)
	assert.Equal(t, float64(1000), report.MinMicroseconds)
	assert.Equal(t, float64(4000), report.MaxMicroseconds)
	assert.Equal(t, float64(3), report.Throughput)
}

func validTestConfig() config {
	cfg := defaultConfig()
	cfg.node = "node3"
	return cfg
}
