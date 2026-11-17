// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/cache"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestInvalidationsLocalDispatch(t *testing.T) {
	for _, prefix := range []string{"", "invalidation"} {
		h := NewInvalidations(nil, prefix, nil)
		var got []cache.Event
		h.Watch("status", func(e cache.Event) { got = append(got, e) })
		h.Watch("other", func(cache.Event) { t.Error("unexpected scope") })
		require.NoError(t, h.Publish("status", 7, "key"))
		require.NoError(t, h.PublishAll("status"))
		assert.Equal(t, []cache.Event{{Scope: "status", ID: 7, Keys: []string{"key"}}}, got)
	}
}

func TestInvalidationsWireAndMalformedMessages(t *testing.T) {
	nc := testnats.Connect(t)
	h := NewInvalidations(nc, "invalidation", nil)
	events := make(chan cache.Event, 8)
	h.Watch("status", func(e cache.Event) { events <- e })
	h.Watch("status", func(cache.Event) {})
	h.Watch("data.users.changed", func(cache.Event) {})
	assert.Equal(t, 1, nc.NumSubscriptions())
	for _, payload := range []string{`{`, `{"broadcaster_id":"bad"}`, `{"broadcaster_id":"0"}`} {
		require.NoError(t, nc.Publish("invalidation.status", []byte(payload)))
	}
	peer := NewInvalidations(nc, "invalidation", nil)
	h.Publish("status", 7, "key")
	peer.Publish("status", 8, "key")
	peer.PublishAll("status")
	for _, want := range []cache.Event{
		{Scope: "status", ID: 7, Keys: []string{"key"}},
		{Scope: "status", ID: 8, Keys: []string{"key"}},
	} {
		select {
		case got := <-events:
			assert.Equal(t, want, got)
		case <-time.After(time.Second):
			t.Fatal("missing local or peer event")
		}
	}
	require.NoError(t, nc.Flush())
	assert.Empty(t, events, "a hub ignores the wire echo of its own publish")
}

func TestInvalidationsReconnectFlushesCachesAndChainsHandler(t *testing.T) {
	serverConn := testnats.Connect(t)
	nc, err := nats.Connect(serverConn.ConnectedUrl(), nats.ReconnectWait(time.Millisecond))
	require.NoError(t, err)
	defer nc.Close()
	var chained atomic.Int32
	nc.SetReconnectHandler(func(*nats.Conn) { chained.Add(1) })
	h := NewInvalidations(nc, "invalidation", nil)
	c := cache.New[int](16, time.Minute).InvalidateOn(h, cache.UserKeys(""), "status")
	defer c.Close()
	c.Set("7", 1)
	require.NoError(t, nc.ForceReconnect())
	require.Eventually(t, func() bool { return chained.Load() > 0 }, time.Second, time.Millisecond)
	_, found := c.Get("7")
	assert.False(t, found)
}

func TestInvalidationsWarnAfterLocalDispatchOnWireFailure(t *testing.T) {
	nc := testnats.Connect(t)
	core, logs := observer.New(zap.WarnLevel)
	h := NewInvalidations(nc, "invalidation", zap.New(core))
	called := false
	h.Watch("status", func(cache.Event) { called = true })
	nc.Close()
	h.Publish("status", 7)
	assert.True(t, called)
	assert.Equal(t, 1, logs.Len())
}

type invalidationPublisher struct {
	Publisher
	err error
}

func (p invalidationPublisher) PublishOwned(_ context.Context, _ string, payload []byte) error {
	if p.err == nil {
		clear(payload)
	}
	return p.err
}
func (p invalidationPublisher) PublishOwnedWithID(ctx context.Context, subject, _ string, payload []byte) error {
	return p.PublishOwned(ctx, subject, payload)
}

func TestInvalidatingPublisher(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		payload string
		err     error
		want    int
	}{
		{name: "success", ctx: t.Context(), payload: `{"user_id":7}`, want: 1},
		{name: "skipped", ctx: SkipInvalidation(t.Context()), payload: `{"user_id":7}`},
		{name: "failed", ctx: t.Context(), payload: `{"user_id":7}`, err: errors.New("publish failed"), want: 1},
		{name: "malformed", ctx: t.Context(), payload: `{`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, confirmed := range []bool{false, true} {
				h := NewInvalidations(nil, "", nil)
				var got []cache.Event
				h.Watch("data.users.changed", func(e cache.Event) { got = append(got, e) })
				pub := h.Publisher(invalidationPublisher{err: tc.err})
				publish := func() error { return PublishRaw(tc.ctx, pub, "data.users.changed", []byte(tc.payload)) }
				if confirmed {
					publish = func() error {
						return PublishConfirmed(tc.ctx, pub, Publication{Subject: "data.users.changed", ID: "id", Payload: []byte(tc.payload)})
					}
				}
				assert.ErrorIs(t, publish(), tc.err)
				assert.Len(t, got, tc.want)
			}
		})
	}
}

func TestInvalidationsChanges(t *testing.T) {
	for _, subject := range []string{"data.users.changed", "data.commands.changed"} {
		h := NewInvalidations(nil, "", nil)
		events := make(chan cache.Event, 1)
		h.Watch(subject, func(e cache.Event) { events <- e })
		sub := &feedSubscriber{messages: make(chan *Message, 1)}
		require.NoError(t, h.Changes(t.Context(), sub, subject))
		msg := NewMessage("id", []byte(`{"user_id":7,"name":"ignored"}`))
		sub.messages <- msg
		close(sub.messages)
		select {
		case got := <-events:
			assert.Equal(t, cache.Event{Scope: subject, ID: 7}, got)
		case <-time.After(time.Second):
			t.Fatal("missing change invalidation")
		}
		awaitSignal(t, msg.Acked(), "change was not acknowledged")
	}
}

func TestInvalidationsChangesBroadcast(t *testing.T) {
	f := newBusFixture(t)
	events := make(chan cache.Event, 4)
	subjects := []string{"data.test.first", "data.test.second"}
	for range 2 {
		h := NewInvalidations(nil, "", nil)
		for _, subject := range subjects {
			h.Watch(subject, func(e cache.Event) { events <- e })
		}
		require.NoError(t, h.Changes(SkipInvalidation(t.Context()), openSubscriber(t, f, ""), subjects...))
	}
	for _, subject := range subjects {
		f.appendMessage(t, subject, `{"user_id":7}`)
	}
	for range 4 {
		select {
		case e := <-events:
			assert.Equal(t, uint64(7), e.ID)
		case <-time.After(3 * time.Second):
			t.Fatal("replica missed change")
		}
	}
}
