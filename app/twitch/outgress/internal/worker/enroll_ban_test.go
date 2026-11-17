// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const missingAuthorizationBody = `{"error":"Forbidden","status":403,"message":"subscription missing proper authorization"}`

func chatForbiddenTransport(t *testing.T, banList string) subscriptionTransport {
	return func(r *http.Request) (*http.Response, error) {
		if strings.HasPrefix(r.URL.Path, "/helix/moderation/banned") {
			return subscriptionResponse(http.StatusOK, banList), nil
		}
		if r.Method == http.MethodGet {
			return subscriptionResponse(http.StatusOK, `{"data":[]}`), nil
		}
		body, _ := io.ReadAll(r.Body)
		var spec twitch.SubSpec
		require.NoError(t, codec.Unmarshal(body, &spec))
		if spec.BotChat() {
			return subscriptionResponse(http.StatusForbidden, missingAuthorizationBody), nil
		}
		return subscriptionResponse(http.StatusAccepted, `{}`), nil
	}
}

type recordingAuthority struct {
	mu      sync.Mutex
	actions []string
}

func (a *recordingAuthority) serve(t *testing.T) *nats.Conn {
	t.Helper()
	serving := servingSnapshot()
	nc := testnats.Connect(t)
	require.NoError(t, bus.Serve(bus.RPCWiring{NC: nc, Log: zap.NewNop(), Queue: "users"}, usersrpc.LifecycleSubject, func(_ context.Context, r usersrpc.LifecycleRequest) usersrpc.LifecycleReply {
		a.mu.Lock()
		a.actions = append(a.actions, r.Action)
		a.mu.Unlock()
		state := serving.State
		if r.Action == "ban" {
			state.ChatRestriction = lifecycle.RestrictionBanned
		}
		return usersrpc.LifecycleReply{AccountCreatedAt: serving.AccountCreatedAt, State: state, Applied: r.Action != "get"}
	}))
	require.NoError(t, nc.Flush())
	return nc
}

func (a *recordingAuthority) recorded(action string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Contains(a.actions, action)
}

func TestChatEnrollForbiddenRecordsOnlyAConfirmedBan(t *testing.T) {
	cases := []struct {
		name      string
		banList   string
		wantState string
		wantBan   bool
	}{
		{name: "bot on the ban list stops enrollment as banned", banList: `{"data":[{"user_id":"` + testBot + `"}]}`, wantState: subStateBanned, wantBan: true},
		{name: "bot absent from the ban list stays failing", banList: `{"data":[]}`, wantState: subStateFailing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := verifyWorker(t, chatForbiddenTransport(t, tc.banList), valkeytest.Spawn(t))
			authority := &recordingAuthority{}
			w.lifecycleNC = authority.serve(t)

			require.NoError(t, w.HandleAuthzGranted(grantEvent(t)))

			state, _ := storedSubState(t, w)
			assert.Equal(t, tc.wantState, state)
			assert.Equal(t, tc.wantBan, authority.recorded("ban"))
		})
	}
}
