// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestLevelRPCRequestsAllOwnersConcurrently(t *testing.T) {
	t.Setenv("NODE_NAME", "levels-test")
	nc := testnats.Connect(t)
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	for _, owner := range []struct {
		service rpc.LevelService
		field   string
	}{
		{rpc.UsersLevels, "accounts.onboarded"}, {rpc.ModulesLevels, "modules.rows|quotes|on"}, {rpc.CommandsLevels, "custom.channels"},
	} {
		source := analytics.NewLevelSource(analytics.Owner{Service: owner.service, Owned: analytics.Owned{owner.field}, Seed: func(ctx context.Context) (analytics.Tally, error) {
			started <- struct{}{}
			select {
			case <-release:
				return analytics.Tally{owner.field: 5, "accounts.total|private": 9}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}})
		require.NoError(t, source.Serve(bus.RPCWiring{NC: nc, Queue: string(owner.service), Log: zap.NewNop()}))
	}
	go func() {
		for range 3 {
			<-started
		}
		close(release)
	}()
	values, err := analytics.RequestLevels(nc)(t.Context())
	require.NoError(t, err)
	require.Equal(t, analytics.Tally{"accounts.onboarded": 5, "modules.rows|quotes|on": 5, "custom.channels": 5}, values)

	reply, err := bus.RequestJSON[rpc.LevelsReply](t.Context(), nc, rpc.UsersLevels.LevelsSubject()+".node.levels-test", rpc.LevelsRequest{})
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"accounts.onboarded": 5}, reply.Levels)
}

func TestLevelRPCFailsWholeReadOnOwnerFailure(t *testing.T) {
	for _, failed := range []rpc.LevelService{rpc.UsersLevels, rpc.ModulesLevels, rpc.CommandsLevels} {
		t.Run(string(failed), func(t *testing.T) {
			nc := testnats.Connect(t)
			for _, service := range []rpc.LevelService{rpc.UsersLevels, rpc.ModulesLevels, rpc.CommandsLevels} {
				source := analytics.NewLevelSource(analytics.Owner{Service: service, Seed: func(context.Context) (analytics.Tally, error) {
					if service == failed {
						return nil, errors.New("SQL unavailable")
					}
					return analytics.Tally{}, nil
				}})
				require.NoError(t, source.Serve(bus.RPCWiring{NC: nc, Log: zap.NewNop()}))
			}
			values, err := analytics.RequestLevels(nc)(t.Context())
			require.ErrorContains(t, err, "SQL unavailable")
			require.Nil(t, values)
		})
	}
}

func TestLevelReaderPropagatesOwnerError(t *testing.T) {
	want := errors.New("owner unavailable")
	reader := analytics.NewReader(nil, func(context.Context) (analytics.Tally, error) { return nil, want })
	now := time.Now()
	rows, err := reader.Read(t.Context(), analytics.Query{Metrics: []catalog.Metric{catalog.AccountsOnboarded}, Window: catalog.Day, From: catalog.PeriodStart(catalog.Day, now), Now: now})
	require.ErrorIs(t, err, want)
	require.Nil(t, rows)
}
