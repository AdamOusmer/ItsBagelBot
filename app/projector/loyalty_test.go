// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"testing"

	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeRequest(t *testing.T, reply loyaltyrpc.Reply) *loyaltyCounters {
	t.Helper()
	body, err := codec.Marshal(reply)
	require.NoError(t, err)
	return &loyaltyCounters{
		prefix: "bagel.rpc.loyalty",
		request: func(ctx context.Context, subject string, data []byte) (*nats.Msg, error) {
			return &nats.Msg{Data: body}, nil
		},
	}
}

func TestLoyaltyCountersGetFound(t *testing.T) {
	l := fakeRequest(t, loyaltyrpc.Reply{Found: true, Counter: &loyaltyrpc.Counter{Name: "messages_processed", Value: 9412}})
	v, ok := l.get(context.Background(), "123", "messages_processed")
	assert.True(t, ok)
	assert.Equal(t, int64(9412), v)
}

func TestLoyaltyCountersGetNotFound(t *testing.T) {
	l := fakeRequest(t, loyaltyrpc.Reply{Found: false})
	v, ok := l.get(context.Background(), "123", "commands_answered")
	assert.True(t, ok)
	assert.Equal(t, int64(0), v)
}

func TestLoyaltyCountersGetErrorReply(t *testing.T) {
	l := fakeRequest(t, loyaltyrpc.Reply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "bad request")})
	_, ok := l.get(context.Background(), "123", "mod_actions")
	assert.False(t, ok)
}

func TestLoyaltyCountersGetTransportFailure(t *testing.T) {
	l := &loyaltyCounters{
		prefix: "bagel.rpc.loyalty",
		request: func(ctx context.Context, subject string, data []byte) (*nats.Msg, error) {
			return nil, errors.New("no responders")
		},
	}
	_, ok := l.get(context.Background(), "123", "messages_processed")
	assert.False(t, ok)
}

func TestLoyaltyCountersGetMalformedReply(t *testing.T) {
	l := &loyaltyCounters{
		prefix: "bagel.rpc.loyalty",
		request: func(ctx context.Context, subject string, data []byte) (*nats.Msg, error) {
			return &nats.Msg{Data: []byte("not json")}, nil
		},
	}
	_, ok := l.get(context.Background(), "123", "messages_processed")
	assert.False(t, ok)
}
