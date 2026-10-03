// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ratelimit

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

func newValkeyTestClient(t *testing.T) valkey.Client {
	t.Helper()
	address := os.Getenv("VALKEY_TEST_ADDR")
	if address == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{address},
		Password:    os.Getenv("VALKEY_TEST_PASSWORD"),
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func TestNewSpecRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		capacity float64
		refill   float64
	}{
		{name: "rejects zero capacity", capacity: 0, refill: 1},
		{name: "rejects zero refill rate", capacity: 1, refill: 0},
		{name: "rejects fractional capacity", capacity: 1.5, refill: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Panics(t, func() { NewSpec(tc.capacity, tc.refill) })
		})
	}
}

type orderedBuckets struct {
	t      *testing.T
	ctx    context.Context
	client valkey.Client
	first  string
	second string
}

func (o orderedBuckets) seed(key, tokens string) {
	future := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	require.NoError(o.t, o.client.Do(o.ctx, o.client.B().Hset().Key(key).FieldValue().
		FieldValue("tokens", tokens).FieldValue("last_ms", future).Build()).Error())
}

func (o orderedBuckets) state(key string) map[string]string {
	state, err := o.client.Do(o.ctx, o.client.B().Hgetall().Key(key).Build()).AsStrMap()
	require.NoError(o.t, err)
	return state
}

func (o orderedBuckets) corrupt(key string) {
	require.NoError(o.t, o.client.Do(o.ctx, o.client.B().Set().Key(key).Value("wrong-type").Build()).Error())
}

func TestLimiterAllowOrderedIsAtomicAcrossBothBuckets(t *testing.T) {
	client := newValkeyTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	prefix := "test:outgress:limiter:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	buckets := orderedBuckets{t: t, ctx: ctx, client: client, first: prefix + ":first", second: prefix + ":second"}
	t.Cleanup(func() { client.Do(context.Background(), client.B().Del().Key(buckets.first, buckets.second).Build()) })

	limiter := New(client)
	spec := NewSpec(2, 0.001)
	first, second := spec.ForKey(buckets.first), spec.ForKey(buckets.second)

	denied, err := limiter.AllowOrdered(ctx, first, second)
	require.NoError(t, err)
	require.Zero(t, denied, "fresh pair")

	buckets.seed(buckets.first, "0")
	secondBefore := buckets.state(buckets.second)
	denied, err = limiter.AllowOrdered(ctx, first, second)
	require.NoError(t, err)
	assert.Equal(t, uint8(1), denied, "first-empty pair")
	assert.Equal(t, secondBefore, buckets.state(buckets.second), "second bucket changed after first denial")

	buckets.seed(buckets.first, "2")
	buckets.seed(buckets.second, "0")
	denied, err = limiter.AllowOrdered(ctx, first, second)
	require.NoError(t, err)
	assert.Equal(t, uint8(2), denied, "second-empty pair")
	assert.Equal(t, "2", buckets.state(buckets.first)["tokens"], "atomic fallback")

	buckets.seed(buckets.first, "2")
	buckets.corrupt(buckets.second)
	_, err = limiter.AllowOrdered(ctx, first, second)
	require.Error(t, err, "wrong-type second bucket")
	assert.Equal(t, "2", buckets.state(buckets.first)["tokens"], "first tokens after second-key error")
}
