// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	pkgvalkey "ItsBagelBot/pkg/valkey"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestGossipCallsRecordReplyStatuses(t *testing.T) {
	vc := valkeytest.Spawn(t)
	nc := bustest.NATS(t)
	rec := analytics.New(vc, zap.NewNop(), nil, analytics.WithoutTicker())
	g := NewGossipRPC(nc, "g")
	g.SetAnalytics(rec)
	for _, status := range catalog.Enum(catalog.KindFetch)[:6] {
		_, err := nc.Subscribe(gossiprpc.Subject("g", "custom", status), func(msg *nats.Msg) {
			_ = msg.Respond([]byte(`{"status":"` + status + `"}`))
		})
		require.NoError(t, err)
		require.NoError(t, nc.Flush())
		var reply gossiprpc.CustomFetchReply
		require.NoError(t, g.Call(context.Background(), GossipRoute{Provider: "custom", Endpoint: status}, gossiprpc.Request{}, &reply))
		require.Equal(t, status, string(reply.Status))
	}
	nc.Close()
	var reply gossiprpc.CustomFetchReply
	require.Error(t, g.Call(context.Background(), GossipRoute{Provider: "custom", Endpoint: "fetch"}, gossiprpc.Request{}, &reply))
	rec.Close()
	counts, err := pkgvalkey.HashAllInts(context.Background(), vc, catalog.CounterKey(time.Now()))
	require.NoError(t, err)
	for _, status := range catalog.Enum(catalog.KindFetch) {
		require.EqualValues(t, 1, counts["provider.calls|custom|"+status], status)
	}
}

func TestProviderFetchStatusUsesTypedReply(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  any
		err  error
		want string
	}{
		{name: "transport", out: &gossiprpc.CustomFetchReply{Status: gossiprpc.FetchOK}, err: errors.New("transport failed"), want: "error"},
		{name: "refusal", out: &gossiprpc.CustomFetchReply{Status: gossiprpc.FetchOK}, err: bus.RPCReplyError{Message: "refused"}, want: "error"},
		{name: "typed custom", out: &gossiprpc.CustomFetchReply{Status: gossiprpc.FetchDenied}, want: "denied"},
		{name: "other typed reply", out: &gossiprpc.SpotifyPlayerReply{}, want: "ok"},
		{name: "status on other reply", out: &struct{ Status string }{Status: "denied"}, want: "ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, providerFetchStatus(tc.out, tc.err))
		})
	}
}
