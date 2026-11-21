// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config_test

import (
	"os"
	"testing"

	"ItsBagelBot/app/gossip/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const temperatureEnv = "GEMINI_TTS_TEMPERATURE"

type temperatureCase struct {
	name    string
	raw     string
	want    float64
	unset   bool
	invalid bool
}

func TestTTSTemperatureFromEnv(t *testing.T) {
	for _, tc := range []temperatureCase{
		{name: "unset defaults to 1.5", unset: true, want: 1.5},
		{name: "explicit zero", raw: "0"},
		{name: "fraction", raw: "0.75", want: 0.75},
		{name: "inclusive upper bound", raw: "2", want: 2},
		{name: "above upper bound", raw: "2.5", invalid: true},
		{name: "negative", raw: "-0.01", invalid: true},
		{name: "empty", invalid: true},
		{name: "non-numeric", raw: "high", invalid: true},
		{name: "NaN", raw: "NaN", invalid: true},
		{name: "infinite", raw: "+Inf", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) { tc.check(t) })
	}
}

func (tc temperatureCase) check(t *testing.T) {
	t.Helper()
	t.Setenv(temperatureEnv, tc.raw)
	if tc.unset {
		require.NoError(t, os.Unsetenv(temperatureEnv))
	}
	cfg, err := config.Load()
	if tc.invalid {
		require.ErrorContains(t, err, temperatureEnv)
		return
	}
	require.NoError(t, err)
	assert.Equal(t, tc.want, cfg.GeminiTTSTemperature)
}
