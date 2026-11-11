// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestEndpointHandlerUsesRegisteredIdentityAndLeavesReplyTime(t *testing.T) {
	app, err := newrelic.NewApplication(func(cfg *newrelic.Config) {
		cfg.AppName = "gossip-test"
		cfg.Enabled = false
	})
	require.NoError(t, err)
	t.Cleanup(func() { app.Shutdown(time.Second) })
	txn := app.StartTransaction("rpc rpc.gossip")
	defer txn.End()
	ctx, cancel := context.WithTimeout(newrelic.NewContext(context.Background(), txn), time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	called := false
	ep := provider.Endpoint{Name: "stats", Handle: func(fetchCtx context.Context, req gossiprpc.Request) any {
		called = true
		assert.Equal(t, "gossip fortnite.stats", txn.Name())
		assert.Equal(t, "private-player", req.Account)
		fetchDeadline, ok := fetchCtx.Deadline()
		require.True(t, ok)
		assert.True(t, fetchDeadline.Before(deadline))
		assert.NoError(t, fetchCtx.Err())
		core.RecordFetchError(fetchCtx, context.DeadlineExceeded)
		return gossiprpc.FortniteStatsReply{Error: "stats lookup failed"}
	}}
	// Respond has no subscription here; the isolated handler still exercises naming,
	// decoding, timeout shaping and error classification with monitoring disabled.
	endpoint := serverEndpoint{provider: "fortnite", subject: "custom-prefix.fortnite.stats", spec: ep, log: zap.NewNop()}
	endpoint.handle(ctx, &nats.Msg{Data: []byte(`{"account":"private-player"}`)})
	assert.True(t, called)
}

func TestEndpointHandlerRejectsInvalidRequestsBeforeFetching(t *testing.T) {
	called := false
	ep := provider.Endpoint{Name: "stats", Handle: func(context.Context, gossiprpc.Request) any {
		called = true
		return nil
	}}
	endpoint := serverEndpoint{provider: "fortnite", subject: "rpc.gossip.fortnite.stats", spec: ep, log: zap.NewNop()}
	endpoint.handle(context.Background(), &nats.Msg{Data: []byte(`{"account":`)})
	assert.False(t, called)
}

func TestUnencodableReplyBecomesAnInternalFailure(t *testing.T) {
	body := encode("rpc.gossip.fortnite.stats", func() {}, zap.NewNop())
	assert.JSONEq(t, `{"error":"internal error"}`, string(body))
	assert.Equal(t, "internal_error", core.ReplyOutcome(context.Background(), body))
}
