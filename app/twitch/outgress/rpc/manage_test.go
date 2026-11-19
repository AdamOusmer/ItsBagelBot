// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const followagePermissionDenied = `{"error":"Unauthorized","status":401,"message":"user does not have the required permission for the broadcaster"}`

func newTestManage(api *twitch.Client) *Manage {
	return &Manage{twitch: api, log: zap.NewNop(), limiter: &chatterFakeLimiter{allow: true}, botID: "456"}
}

func TestFollowageFailureLogLevel(t *testing.T) {
	cases := []struct {
		name         string
		trial        bool
		status       int
		body         string
		wantLevel    zapcore.Level
		wantExpected bool
	}{
		{"trial permission denial", true, http.StatusUnauthorized, followagePermissionDenied, zapcore.InfoLevel, true},
		{"registered permission denial", false, http.StatusUnauthorized, followagePermissionDenied, zapcore.WarnLevel, false},
		{"trial outage", true, http.StatusServiceUnavailable, `{"message":"unavailable"}`, zapcore.WarnLevel, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := twitch.NewClient("client", twitch.NewStaticTokenSource("app"), twitch.NewStaticTokenSource("bot"), nil)
			api.SetTransport(chatterRevocationTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}))
			core, logs := observer.New(zapcore.DebugLevel)
			m := newTestManage(api)
			m.log = zap.New(core)

			reply := m.handleFollowage(t.Context(), outgressrpc.FollowageRequest{BroadcasterID: "channel", TargetID: "viewer", Trial: tc.trial})

			require.Equal(t, "lookup failed", reply.Error)
			entries := logs.FilterMessage("followage lookup failed").All()
			require.Len(t, entries, 1)
			require.Equal(t, tc.wantLevel, entries[0].Level)
			_, hasExpected := entries[0].ContextMap()["expected"]
			require.Equal(t, tc.wantExpected, hasExpected)
		})
	}
}

func TestChannelGetVerifyReturnsVerification(t *testing.T) {
	registry := channels.New(valkeytest.New(t).Client())
	t.Cleanup(registry.Close)
	registry.SeedPause(false)
	var gotRepair bool
	m := newTestManage(nil)
	m.registry = registry
	m.verify = func(_ context.Context, _ string, repair bool) (manage.Verification, error) {
		gotRepair = repair
		return manage.Verification{Expected: 3, Present: 2, Missing: []string{"channel.cheer"}, Repairing: repair}, nil
	}

	reply := m.handleChannelGet(t.Context(), manage.ChannelRequest{BroadcasterID: "42", Verify: true, Repair: true})

	require.Equal(t, &manage.Verification{Expected: 3, Present: 2, Missing: []string{"channel.cheer"}, Repairing: true}, reply.Verification)
	require.True(t, gotRepair)

	plain := m.handleChannelGet(t.Context(), manage.ChannelRequest{BroadcasterID: "42"})
	require.Nil(t, plain.Verification)
}
