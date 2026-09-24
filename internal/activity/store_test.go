// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package activity

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

func TestTruncateBytes(t *testing.T) {
	assert.Equal(t, "short", truncateBytes("short", 40))
	assert.Equal(t, "", truncateBytes("", 40))
	assert.Equal(t, "abc", truncateBytes("abcdef", 3))

	s := "ab" + "❤"
	got := truncateBytes(s, 4)
	assert.True(t, len(got) <= 4)
	assert.True(t, strings_ValidUTF8(got))
}

func strings_ValidUTF8(s string) bool {
	return strings.ToValidUTF8(s, "") == s
}

func TestEncodeDecodeRowRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	row := Row{Kind: KindCommand, Text: "!bagel answered @novaburst", Meta: "41ms", At: now, DurationMS: 41}

	data, err := encodeRow(row)
	require.NoError(t, err)

	got, ok := decodeRow(string(data))
	require.True(t, ok)
	assert.Equal(t, row.Kind, got.Kind)
	assert.Equal(t, row.Text, got.Text)
	assert.Equal(t, row.Meta, got.Meta)
	assert.Equal(t, row.DurationMS, got.DurationMS)
	assert.True(t, row.At.Equal(got.At))
}

func TestEncodeRowTruncatesOversizeFields(t *testing.T) {
	row := Row{Kind: KindEvent, Text: strings.Repeat("x", 200), Meta: strings.Repeat("y", 200), At: time.Now()}
	data, err := encodeRow(row)
	require.NoError(t, err)
	got, ok := decodeRow(string(data))
	require.True(t, ok)
	assert.LessOrEqual(t, len(got.Text), maxTextBytes)
	assert.LessOrEqual(t, len(got.Meta), maxMetaBytes)
}

func TestDecodeRowRejectsGarbage(t *testing.T) {
	_, ok := decodeRow("not json")
	assert.False(t, ok)
}

func TestMedian(t *testing.T) {
	assert.Nil(t, median(nil))
	assert.Nil(t, median([]string{"not-a-number"}))

	m := median([]string{"10", "30", "20"})
	require.NotNil(t, m)
	assert.Equal(t, 20, *m)

	m = median([]string{"10", "junk", "30"})
	require.NotNil(t, m)
	assert.Equal(t, 30, *m)
}

func TestKeyBuilders(t *testing.T) {
	assert.Equal(t, "activity:feed:123", feedKey("123"))
	assert.Equal(t, "activity:latency:123", latencyKey("123"))
	assert.Equal(t, "activity:dropped:123", droppedKey("123"))
}

func newTestClient(t *testing.T) valkey.Client {
	t.Helper()
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	c, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{addr},
		Password:    os.Getenv("VALKEY_TEST_PASSWORD"),
	})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}

func TestStoreEmitAndRead(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	s := NewStore(client)
	channelID := "activity-store-test-" + time.Now().Format("150405.000000000")

	s.Emit(ctx, channelID, Row{Kind: KindAutomod, Text: "ban issued", At: time.Now()})
	s.Emit(ctx, channelID, Row{Kind: KindCommand, Text: "!bagel answered @novaburst", Meta: "41ms", At: time.Now(), DurationMS: 41})
	s.Emit(ctx, channelID, Row{Kind: KindCommand, Text: "!bagel answered @kip", Meta: "21ms", At: time.Now(), DurationMS: 21})

	feed, err := s.Read(ctx, channelID)
	require.NoError(t, err)
	require.Len(t, feed.Rows, 3)
	assert.Equal(t, "!bagel answered @kip", feed.Rows[0].Text)
	require.NotNil(t, feed.MedianMS)
	assert.Equal(t, 41, *feed.MedianMS)
	assert.Zero(t, feed.Dropped)

	ttl, err := client.Do(ctx, client.B().Ttl().Key(feedKey(channelID)).Build()).AsInt64()
	require.NoError(t, err)
	assert.Positive(t, ttl)
}

func TestStoreEmitDropsOnCanceledContext(t *testing.T) {
	client := newTestClient(t)
	s := NewStore(client)
	channelID := "activity-store-drop-test-" + time.Now().Format("150405.000000000")

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	s.Emit(canceled, channelID, Row{Kind: KindEvent, Text: "x", At: time.Now()})

	assert.Equal(t, uint64(1), s.Dropped())
}
