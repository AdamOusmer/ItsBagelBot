// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"os"
	"testing"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/repository"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	_ "github.com/mattn/go-sqlite3"
)

func TestPageSetRoundTripIntegration(t *testing.T) {
	nc := dialDashboardIntegrationBroker(t)
	ctx := context.Background()

	client := testdb.Open(t, "dashboardrpc-commandspage", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewUsers(client, nil, bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "Mavey", "mavey@concordia.ca"))

	prefix := "bagel.rpc.users.dashboard." + nuid.Next()
	invalidationPrefix := "bagel.invalidate." + nuid.Next()

	require.NoError(t, SubscribeDashboard(Wiring{
		RPCWiring: bus.RPCWiring{NC: nc, Queue: "dashboardrpc-test-" + nuid.Next(), Log: zap.NewNop()},
		Repo:      repo,
	}, prefix, invalidationPrefix))
	require.NoError(t, nc.Flush())

	tests := []struct {
		verb, field string
		hidden      func(repository.UserView) bool
	}{
		{"commands_page_set", "commands_page_hidden", func(v repository.UserView) bool { return v.CommandsPageHidden }},
		{"leaderboard_page_set", "leaderboard_page_hidden", func(v repository.UserView) bool { return v.LeaderboardPageHidden }},
	}
	for _, tc := range tests {
		t.Run(tc.verb, func(t *testing.T) {
			reply, err := bus.RequestJSON[map[string]any](ctx, nc, prefix+"."+tc.verb, map[string]any{
				"broadcaster_user_id": "1001",
				tc.field:              true,
			})
			require.NoError(t, err)
			require.Equal(t, true, reply["ok"])

			view, err := repo.Get(ctx, 1001)
			require.NoError(t, err)
			require.True(t, tc.hidden(view))

			state, err := bus.RequestJSON[map[string]any](ctx, nc, prefix+".state_get", usersrpc.GrantHasRequest{
				BroadcasterUserID: "1001",
			})
			require.NoError(t, err)
			require.Equal(t, true, state[tc.field])
		})
	}
}

func dialDashboardIntegrationBroker(t *testing.T) *nats.Conn {
	t.Helper()
	url := os.Getenv("NATS_INTEGRATION_URL")
	if url == "" {
		t.Skip("NATS_INTEGRATION_URL is not set")
	}

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return nc
}
