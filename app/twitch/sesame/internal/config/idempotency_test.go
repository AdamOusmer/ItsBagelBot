// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/internal/config"
	"ItsBagelBot/pkg/bus"
	"github.com/stretchr/testify/require"
)

func TestIdempotencyRetentionCoversStreamsAndAcceptsLongerConfiguration(t *testing.T) {
	for _, value := range []string{"0", "-1s", "2m", "24h"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SESAME_IDEMPOTENCY_TTL", value)
			ttl, err := time.ParseDuration(value)
			require.NoError(t, err)
			require.Equal(t, max(ttl, bus.TwitchEffectRetention()), config.Load().IdempotencyTTL)
		})
	}
}
