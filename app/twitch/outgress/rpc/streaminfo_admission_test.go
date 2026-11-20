// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/channelruntime"
	"ItsBagelBot/internal/domain/lifecycle"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"github.com/stretchr/testify/require"
)

type streamInfoCallerRuntime struct{}

func (streamInfoCallerRuntime) Get(_ context.Context, id uint64) (channelruntime.Snapshot, error) {
	return channelruntime.Snapshot{
		Registered: id == 123,
		State: lifecycle.State{
			Version:         1,
			DesiredEnabled:  true,
			RuntimeState:    lifecycle.Active,
			GrantHealth:     lifecycle.GrantHealthy,
			ChatRestriction: lifecycle.RestrictionNone,
		},
	}, nil
}

func TestNamedStreamInfoAdmitsCallerAndReadsTarget(t *testing.T) {
	api := twitch.NewClient("client", twitch.NewStaticTokenSource("app"), nil, nil)
	api.SetTransport(chatterRevocationTransport(streamInfoTargetTransport(t)))
	m := newTestManage(api)
	m.runtime = streamInfoCallerRuntime{}

	req := outgressrpc.StreamInfoRequest{CallerBroadcasterID: "123", TargetLogin: "streamer"}
	reply := m.handleStreamInfo(t.Context(), req)
	require.Equal(t, outgressrpc.StreamInfoReply{UserFound: true, Live: true, GameName: "VALORANT"}, reply)

	req.CallerBroadcasterID = "456"
	reply = m.handleStreamInfo(t.Context(), req)
	require.Equal(t, "channel unavailable", reply.Error)
}

func streamInfoTargetTransport(t *testing.T) func(*http.Request) (*http.Response, error) {
	t.Helper()
	return func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/helix/users":
			require.Equal(t, "streamer", req.URL.Query().Get("login"))
			body = `{"data":[{"id":"456"}]}`
		case "/helix/streams":
			require.Equal(t, "456", req.URL.Query().Get("user_id"))
			body = `{"data":[{"type":"live","game_name":"VALORANT"}]}`
		default:
			t.Fatalf("unexpected Twitch API path %q", req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}
}
