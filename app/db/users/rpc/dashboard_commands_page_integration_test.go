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

// TestCommandsPageSetRoundTripIntegration exercises commands_page_set and
// state_get end to end over a live NATS connection. No cursor_set test exists
// to mirror (grep over app/db/users/rpc/*_test.go turns up nothing -- none of
// the setBoolPref verbs are covered today), so per spec this is deliberately
// minimal: just the new verb's write-then-read-back, not the whole dashboard
// surface.
//
// Like pkg/bus's own *_integration_test.go files, this skips without a real
// broker so CI (which sets no NATS_INTEGRATION_URL) never needs one.
func TestCommandsPageSetRoundTripIntegration(t *testing.T) {
	nc := dialDashboardIntegrationBroker(t)
	ctx := context.Background()

	client := testdb.Open(t, "dashboardrpc-commandspage", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	// bustest.Publisher, not nil: Register/SetCommandsPageHidden both call
	// publishChanged, which needs a real bus.Publisher to marshal onto -- a
	// nil one panics on the very first write. The RPC layer itself still
	// talks to the real broker dialed above.
	repo := repository.NewUsers(client, nil, bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })

	require.NoError(t, repo.Register(ctx, 1001, "Mavey", "Mavey", "mavey@concordia.ca"))

	// Unique per run so parallel CI shards (or a re-run against the same
	// broker) never collide on subject or queue group.
	prefix := "bagel.rpc.users.dashboard." + nuid.Next()
	invalidationPrefix := "bagel.invalidate." + nuid.Next()

	require.NoError(t, SubscribeDashboard(Wiring{
		RPCWiring: bus.RPCWiring{NC: nc, Queue: "dashboardrpc-test-" + nuid.Next(), Log: zap.NewNop()},
		Repo:      repo,
	}, prefix, invalidationPrefix))
	// App is left nil: every newrelic.Application method is documented
	// nil-safe, so tracedHandler's StartTransaction/End work without spinning
	// up a real (or fake) collector for a test that only cares about the RPC
	// contract.
	require.NoError(t, nc.Flush())

	reply, err := bus.RequestJSON[map[string]any](ctx, nc, prefix+".commands_page_set", usersrpc.CommandsPageSetRequest{
		BroadcasterUserID: "1001",
		Hidden:            true,
	})
	require.NoError(t, err)
	require.Equal(t, true, reply["ok"])

	// D8: the RPC answers only after the write-through commit, so the repo
	// must already reflect it -- no batcher window to wait out.
	view, err := repo.Get(ctx, 1001)
	require.NoError(t, err)
	require.True(t, view.CommandsPageHidden)

	state, err := bus.RequestJSON[map[string]any](ctx, nc, prefix+".state_get", usersrpc.GrantHasRequest{
		BroadcasterUserID: "1001",
	})
	require.NoError(t, err)
	require.Equal(t, true, state["commands_page_hidden"])
}

// dialDashboardIntegrationBroker connects to the broker the integration tests
// need, or skips when none is configured. Mirrors pkg/bus's
// dialIntegrationBroker; duplicated rather than exported across packages for
// one test each.
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
