// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCounterMutationsPublishOnlyCounterInvalidations(t *testing.T) {
	h := newHarness(t)
	sub, err := h.nc.SubscribeSync(invalidationPrefix + ".>")
	require.NoError(t, err)
	require.NoError(t, h.nc.Flush())
	for _, step := range []struct {
		verb string
		req  loyaltyrpc.Request
		keys []string
	}{
		{"counter.create", loyaltyrpc.Request{Name: "!Hugs", Scope: data.CounterScopeViewer}, []string{"hugs"}},
		{"counter.set", loyaltyrpc.Request{Name: "hugs", ViewerID: "7", Value: 20}, []string{"hugs"}},
		{"counter.rename", loyaltyrpc.Request{Name: "hugs", NewName: "!Waves"}, []string{"hugs", "waves"}},
		{"counter.entry.delete", loyaltyrpc.Request{Name: "waves", ViewerID: "7"}, []string{"waves"}},
		{"counter.delete", loyaltyrpc.Request{Name: "waves"}, []string{"waves"}},
	} {
		step.req.UserID = "2"
		reply := h.call(t, step.verb, step.req)
		require.True(t, reply.Found, step.verb)
		msg, err := sub.NextMsg(3 * time.Second)
		require.NoError(t, err)
		var dto invalidate.DTO
		require.NoError(t, codec.Unmarshal(msg.Data, &dto))
		assert.Equal(t, invalidationPrefix+"."+invalidate.ScopeLoyaltyCounters, msg.Subject)
		assert.Equal(t, invalidate.DTO{BroadcasterID: "2", Keys: step.keys}, dto)
	}
	_, err = sub.NextMsg(50 * time.Millisecond)
	assert.ErrorIs(t, err, nats.ErrTimeout)
}

func TestCounterMutationRefusalDoesNotInvalidate(t *testing.T) {
	h := newHarness(t)
	sub, err := h.nc.SubscribeSync(invalidationPrefix + ".>")
	require.NoError(t, err)
	require.NoError(t, h.nc.Flush())
	reply := h.call(t, "counter.rename", loyaltyrpc.Request{UserID: "2", Name: "missing", NewName: "other"})
	assert.False(t, reply.Found)
	reply = h.call(t, "counter.create", loyaltyrpc.Request{UserID: "2", Name: "bad:name"})
	assert.NotEmpty(t, reply.Error)
	_, err = sub.NextMsg(50 * time.Millisecond)
	assert.ErrorIs(t, err, nats.ErrTimeout)
}
