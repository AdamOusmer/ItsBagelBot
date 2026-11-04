// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"io"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
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

type postedTypes struct {
	*verifyTransport
	mu    sync.Mutex
	kinds []string
}

func (p *postedTypes) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodPost {
		var spec twitch.SubSpec
		body, _ := io.ReadAll(r.Body)
		if err := codec.Unmarshal(body, &spec); err != nil {
			return nil, err
		}
		p.mu.Lock()
		p.kinds = append(p.kinds, spec.Type)
		p.mu.Unlock()
	}
	return p.verifyTransport.RoundTrip(r)
}

func grantedAuthority(t *testing.T) *nats.Conn {
	t.Helper()
	serving := servingSnapshot()
	nc := testnats.Connect(t)
	require.NoError(t, bus.Serve(bus.RPCWiring{NC: nc, Log: zap.NewNop(), Queue: "users"}, usersrpc.LifecycleSubject, func(_ context.Context, r usersrpc.LifecycleRequest) usersrpc.LifecycleReply {
		return usersrpc.LifecycleReply{AccountCreatedAt: serving.AccountCreatedAt, State: serving.State, Applied: r.Action == "grant_restored"}
	}))
	require.NoError(t, nc.Flush())
	return nc
}

func grantEvent(t *testing.T) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(authzUser{UserID: testBroadcaster, EventID: "evt-1", ObservedAt: time.Now().UTC()})
	require.NoError(t, err)
	return &bus.Message{Payload: body}
}

func TestGrantOnHealthyChannelPostsOnlyMissingSubscriptions(t *testing.T) {
	missing := []string{"channel.follow", "channel.ban", "channel.ad_break.begin"}
	var present []string
	specs := append(twitch.ChannelSubscriptions(testBroadcaster, testBot), twitch.ChannelOptionalSubscriptions(testBroadcaster)...)
	for _, spec := range specs {
		if !slices.Contains(missing, spec.Type) {
			present = append(present, spec.Type)
		}
	}
	transport := &postedTypes{verifyTransport: &verifyTransport{present: present}}
	w := verifyWorker(t, transport, valkeytest.Spawn(t))
	require.NoError(t, w.registry.SetSubState(t.Context(), testBroadcaster, subStateOK, ""))
	w.lifecycleNC = grantedAuthority(t)

	require.NoError(t, w.HandleAuthzGranted(grantEvent(t)))

	assert.ElementsMatch(t, missing, transport.kinds)
	state, _ := storedSubState(t, w)
	assert.Equal(t, subStateOK, state)
}

func TestGrantWithEverySubscriptionPresentPostsNothing(t *testing.T) {
	var present []string
	for _, spec := range append(twitch.ChannelSubscriptions(testBroadcaster, testBot), twitch.ChannelOptionalSubscriptions(testBroadcaster)...) {
		present = append(present, spec.Type)
	}
	transport := &postedTypes{verifyTransport: &verifyTransport{present: present}}
	w := verifyWorker(t, transport, valkeytest.Spawn(t))
	w.lifecycleNC = grantedAuthority(t)

	require.NoError(t, w.HandleAuthzGranted(grantEvent(t)))

	assert.Empty(t, transport.kinds)
	state, _ := storedSubState(t, w)
	assert.Equal(t, subStateOK, state)
}

func TestGrantWithFailedListLeavesChannelForTheSweep(t *testing.T) {
	transport := &postedTypes{verifyTransport: &verifyTransport{listErr: true}}
	w := verifyWorker(t, transport, valkeytest.Spawn(t))
	require.NoError(t, w.registry.SetSubState(t.Context(), testBroadcaster, subStateOK, ""))
	w.lifecycleNC = grantedAuthority(t)

	require.NoError(t, w.HandleAuthzGranted(grantEvent(t)))

	assert.Empty(t, transport.kinds)
	state, _ := storedSubState(t, w)
	assert.Equal(t, subStateFailing, state)
}
