// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package activity_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"ItsBagelBot/internal/activity"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

type recordingSink struct{ channels []string }

func (s *recordingSink) Emit(_ context.Context, channelID string, _ activity.Row) {
	s.channels = append(s.channels, channelID)
}

type fixture struct {
	client    valkey.Client
	store     *activity.Store
	channelID string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	client := valkeytest.Client(t)
	return fixture{client: client, store: activity.NewStore(client), channelID: "activity-test-" + strconv.FormatInt(time.Now().UnixNano(), 10)}
}

func (f fixture) push(t *testing.T, key string, elements ...string) {
	t.Helper()
	require.NoError(t, f.client.Do(t.Context(), f.client.B().Lpush().Key(key).Element(elements...).Build()).Error())
}

func (f fixture) read(t *testing.T) activity.Feed {
	t.Helper()
	feed, err := f.store.Read(t.Context(), f.channelID)
	require.NoError(t, err)
	return feed
}

func TestStoreEmitAndRead(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	f.store.Emit(ctx, f.channelID, activity.Row{Kind: activity.KindAutomod, Text: "ban issued", At: time.Now()})
	f.store.Emit(ctx, f.channelID, activity.Row{Kind: activity.KindCommand, Text: "!bagel answered @novaburst", Meta: "41ms", At: time.Now(), DurationMS: 41})
	f.store.Emit(ctx, f.channelID, activity.Row{Kind: activity.KindCommand, Text: "!bagel answered @kip", Meta: "21ms", At: time.Now(), DurationMS: 21})

	feed := f.read(t)
	require.Len(t, feed.Rows, 3)
	assert.Equal(t, "!bagel answered @kip", feed.Rows[0].Text)
	require.NotNil(t, feed.MedianMS)
	assert.Equal(t, 41, *feed.MedianMS)
	assert.Zero(t, feed.Dropped)
	ttl, err := f.client.Do(ctx, f.client.B().Ttl().Key("activity:feed:"+f.channelID).Build()).AsInt64()
	require.NoError(t, err)
	assert.Positive(t, ttl)
}

func TestStoreEmitTruncatesFieldsOnRuneBoundaries(t *testing.T) {
	f := newFixture(t)

	f.store.Emit(t.Context(), f.channelID, activity.Row{Kind: activity.KindEvent, Text: strings.Repeat("x", 200), Meta: "ab" + strings.Repeat("❤", 20), At: time.Now()})

	row := f.read(t).Rows[0]
	assert.Equal(t, strings.Repeat("x", 40), row.Text)
	assert.LessOrEqual(t, len(row.Meta), 14)
	assert.True(t, utf8.ValidString(row.Meta))
}

func TestStoreReadSkipsUndecodableRowsAndLatencies(t *testing.T) {
	cases := []struct {
		name      string
		feed      []string
		latencies []string
		dropped   string
		wantRows  int
		wantMed   *int
		wantDrop  uint64
	}{
		{name: "garbage rows are skipped", feed: []string{"not json", `{"k":"event","x":"ok","m":"","a":"2026-01-02T03:04:05Z","d":0}`, `{"k":"event","a":"not a time"}`}, wantRows: 1},
		{name: "junk latencies are ignored", feed: []string{`{"k":"event","a":"2026-01-02T03:04:05Z"}`}, latencies: []string{"10", "junk", "30"}, wantRows: 1, wantMed: new(30)},
		{name: "only junk latencies yield no median", feed: []string{`{"k":"event","a":"2026-01-02T03:04:05Z"}`}, latencies: []string{"junk"}, wantRows: 1},
		{name: "dropped counter is read back", feed: []string{`{"k":"event","a":"2026-01-02T03:04:05Z"}`}, dropped: "7", wantRows: 1, wantDrop: 7},
		{name: "unparseable dropped counter reads as zero", feed: []string{`{"k":"event","a":"2026-01-02T03:04:05Z"}`}, dropped: "many", wantRows: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.push(t, "activity:feed:"+f.channelID, tc.feed...)
			if len(tc.latencies) > 0 {
				f.push(t, "activity:latency:"+f.channelID, tc.latencies...)
			}
			if tc.dropped != "" {
				require.NoError(t, f.client.Do(t.Context(), f.client.B().Set().Key("activity:dropped:"+f.channelID).Value(tc.dropped).Build()).Error())
			}

			feed := f.read(t)

			assert.Len(t, feed.Rows, tc.wantRows)
			assert.Equal(t, tc.wantMed, feed.MedianMS)
			assert.Equal(t, tc.wantDrop, feed.Dropped)
		})
	}
}

func TestStoreEmitDropsOnCanceledContext(t *testing.T) {
	f := newFixture(t)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	f.store.Emit(canceled, f.channelID, activity.Row{Kind: activity.KindEvent, Text: "x", At: time.Now()})

	assert.Equal(t, uint64(1), f.store.Dropped())
}

func TestSinkRoutesEmitAndFallsBackToNoop(t *testing.T) {
	sink := &recordingSink{}
	activity.SetSink(sink)
	t.Cleanup(func() { activity.SetSink(nil) })

	activity.Emit(t.Context(), "chan-1", activity.Row{Kind: activity.KindTimer})
	activity.SetSink(nil)
	activity.Emit(t.Context(), "chan-2", activity.Row{Kind: activity.KindTimer})

	assert.Equal(t, []string{"chan-1"}, sink.channels)
}
