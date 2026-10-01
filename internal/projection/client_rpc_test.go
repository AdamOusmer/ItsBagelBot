// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/pkg/bus/bustest"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	failedReply     = `{"error":"request failed","code":"internal"}`
	absentUserReply = `{"user_id":"7","error":"user account not found","code":"not_found"}`
)

var rpcSubjects = Subjects{Users: "test.users", Commands: "test.commands", Fetches: "test.fetches"}

type rpcLookup struct {
	name    string
	subject string
	found   string
	absent  string
	lookup  func(context.Context, *Client) (bool, error)
}

var rpcLookups = []rpcLookup{
	{
		name: "user", subject: rpcSubjects.Users,
		found:  `{"status":"premium","is_active":true}`,
		absent: absentUserReply,
		lookup: func(ctx context.Context, c *Client) (bool, error) {
			u, err := c.User(ctx, 7)
			return u.Premium(), err
		},
	},
	{
		name: "command", subject: rpcSubjects.Commands,
		found:  `{"commands":[{"name":"hi","response":"hello","is_active":true}]}`,
		absent: `{"commands":[{"name":"other","response":"x","is_active":true}]}`,
		lookup: func(ctx context.Context, c *Client) (bool, error) {
			_, found, err := c.Command(ctx, 7, "hi")
			return found, err
		},
	},
	{
		name: "fetch", subject: rpcSubjects.Fetches,
		found:  `{"fetches":[{"name":"wx","url":"https://wx.example"}]}`,
		absent: `{"fetches":[]}`,
		lookup: func(ctx context.Context, c *Client) (bool, error) {
			_, found, err := c.FetchDefs(ctx, 7, "wx")
			return found, err
		},
	},
}

func rpcTestClient(t *testing.T) (*Client, *nats.Conn) {
	t.Helper()
	nc := bustest.NATS(t)
	store, _ := newTestStore(t)
	c := NewClient(Config{Store: store, NC: nc, Subjects: rpcSubjects, TTL: time.Minute, Log: zap.NewNop()})
	t.Cleanup(c.Close)
	return c, nc
}

func TestClientRetriesAfterRPCError(t *testing.T) {
	for _, tc := range rpcLookups {
		t.Run(tc.name, func(t *testing.T) {
			c, nc := rpcTestClient(t)
			served := bustest.Respond(t, nc, tc.subject, failedReply, tc.found)
			ctx := context.Background()

			_, err := tc.lookup(ctx, c)
			require.Error(t, err)
			ok, err := tc.lookup(ctx, c)
			require.NoError(t, err)
			require.True(t, ok, "a failed load must not be cached")
			require.EqualValues(t, 2, served.Load())
		})
	}
}

func TestClientCachesAbsentResults(t *testing.T) {
	for _, tc := range rpcLookups {
		t.Run(tc.name, func(t *testing.T) {
			c, nc := rpcTestClient(t)
			served := bustest.Respond(t, nc, tc.subject, tc.absent)
			ctx := context.Background()

			for range 2 {
				ok, err := tc.lookup(ctx, c)
				require.NoError(t, err)
				require.False(t, ok)
			}
			require.EqualValues(t, 1, served.Load(), "absence is cached for the TTL")
		})
	}
}

func TestClientAbsentUserIsStandard(t *testing.T) {
	c, nc := rpcTestClient(t)
	bustest.Respond(t, nc, rpcSubjects.Users, absentUserReply)

	user, err := c.User(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, User{Status: "standard"}, user)
}

func TestLoadChannelServesStandardUserWithoutCachingRPCError(t *testing.T) {
	c, nc := rpcTestClient(t)
	served := bustest.Respond(t, nc, rpcSubjects.Users, failedReply, `{"status":"premium","is_active":true,"locale":"fr"}`)
	ctx := context.Background()

	_, user, err := c.LoadChannel(ctx, 7, false)
	require.NoError(t, err, "a user lookup failure must not fail the message")
	require.Equal(t, User{Status: "standard"}, user)

	_, user, err = c.LoadChannel(ctx, 7, false)
	require.NoError(t, err)
	require.Equal(t, "fr", user.Locale)
	require.EqualValues(t, 2, served.Load())
}
