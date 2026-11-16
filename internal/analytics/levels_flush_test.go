// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	"ItsBagelBot/internal/analytics/analyticstest"
	catalog "ItsBagelBot/internal/domain/analytics"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type flushCapture struct {
	valkey.Client
	writes chan [][]string
}

func (c *flushCapture) DoMulti(_ context.Context, commands ...valkey.Completed) []valkey.ValkeyResult {
	args := make([][]string, len(commands))
	for i, command := range commands {
		args[i] = append([]string(nil), command.Commands()...)
	}
	c.writes <- args
	return make([]valkey.ValkeyResult, len(commands))
}

func TestShiftFlushesPromptlyOnlyWhenTickerEnabled(t *testing.T) {
	field := catalog.Field(catalog.AccountsTotal, "free")
	one := analytics.Tally{field: 1}
	for _, tc := range []struct {
		name       string
		background bool
		before     analytics.Tally
		after      analytics.Tally
	}{
		{"increase", true, nil, one},
		{"decrease", true, one, analytics.Tally{field: 0}},
		{"remove", true, one, nil},
		{"explicit", false, nil, one},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := analyticstest.New(t)
			client := &flushCapture{Client: base.Client, writes: make(chan [][]string, 128)}
			var options []analytics.Option
			if !tc.background {
				options = append(options, analytics.WithoutTicker())
			}
			r := analytics.New(client, zap.NewNop(), nil, options...)
			t.Cleanup(r.Close)
			r.Shift(analytics.Tally{field: 1}, analytics.Tally{field: 1})
			require.Empty(t, client.writes)
			for range 100 {
				r.Shift(tc.before, tc.after)
			}
			if !tc.background {
				require.Empty(t, client.writes)
				r.Flush(t.Context())
			}
			select {
			case commands := <-client.writes:
				require.Equal(t, []string{"HINCRBY", catalog.LevelKey, field}, commands[0][:3])
			case <-time.After(time.Second):
				t.Fatal("level delta did not flush promptly")
			}
		})
	}
}
