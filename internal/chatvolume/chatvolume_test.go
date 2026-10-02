// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package chatvolume_test

import (
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/chatvolume"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	ringWidth        = 60
	typeChatMessage  = "channel.chat.message"
	typeStreamOnline = "stream.online"
)

type ring struct {
	client      valkey.Client
	store       *chatvolume.Store
	broadcaster uint64
}

func newRing(t *testing.T, client valkey.Client) ring {
	t.Helper()
	r := ring{client: client, store: chatvolume.New(client, zap.NewNop()), broadcaster: uint64(time.Now().UnixNano())}
	t.Cleanup(func() { _ = client.Do(t.Context(), client.B().Del().Key(r.key()).Build()).Error() })
	return r
}

func (r ring) key() string { return "chatvol:" + strconv.FormatUint(r.broadcaster, 10) }

func (r ring) seed(t *testing.T, fields map[string]string) {
	t.Helper()
	cmd := r.client.B().Hset().Key(r.key()).FieldValue()
	for field, value := range fields {
		cmd = cmd.FieldValue(field, value)
	}
	require.NoError(t, r.client.Do(t.Context(), cmd.Build()).Error())
}

func (r ring) read(t *testing.T, now time.Time) chatvolume.ChatVolume {
	t.Helper()
	cv, err := r.store.Read(t.Context(), r.broadcaster, now)
	require.NoError(t, err)
	return cv
}

func minuteAt(epoch int64) time.Time { return time.Unix(epoch*60, 0).UTC() }

func slot(epoch int64) string { return strconv.FormatInt(epoch%ringWidth, 10) }

func TestStoreReadEmptyRingIsAllZero(t *testing.T) {
	r := newRing(t, valkeytest.Client(t))

	cv := r.read(t, minuteAt(1_000_000))

	assert.Equal(t, chatvolume.ChatVolume{Buckets: make([]int, ringWidth)}, cv)
}

func TestStoreReadRingSlots(t *testing.T) {
	now := int64(1_000_100)
	cases := []struct {
		name      string
		fields    map[string]string
		wantNow   int
		wantPeak  int
		wantTicks []int
	}{
		{name: "current lap slot is read with its command tick", fields: map[string]string{"a": "1000000", slot(now): "100:7:1"}, wantNow: 7, wantPeak: 7, wantTicks: []int{ringWidth - 1}},
		{name: "slot written on an earlier lap reads as zero", fields: map[string]string{"a": "1000000", slot(now): "40:42:1"}},
		{name: "missing anchor reads as zero", fields: map[string]string{slot(now): "100:9:1"}},
		{name: "empty slot value is ignored", fields: map[string]string{"a": "1000000", slot(now): ""}},
		{name: "slot without a handled flag is ignored", fields: map[string]string{"a": "1000000", slot(now): "100:2"}},
		{name: "non numeric delta is ignored", fields: map[string]string{"a": "1000000", slot(now): "x:2:0"}},
		{name: "non numeric count is ignored", fields: map[string]string{"a": "1000000", slot(now): "100:x:0"}},
		{name: "trailing segments are tolerated", fields: map[string]string{"a": "1000000", slot(now): "100:2:0:extra"}, wantNow: 2, wantPeak: 2},
		{name: "peak is the busiest minute of the hour", fields: map[string]string{"a": "1000000", slot(now): "100:3:0", slot(now - 5): "95:9:0"}, wantNow: 3, wantPeak: 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRing(t, valkeytest.Client(t))
			r.seed(t, tc.fields)

			cv := r.read(t, minuteAt(now))

			assert.Equal(t, tc.wantNow, cv.Now)
			assert.Equal(t, tc.wantPeak, cv.Peak)
			assert.Equal(t, tc.wantTicks, cv.CommandTicks)
		})
	}
}

type observed struct {
	typ     string
	after   time.Duration
	handled bool
}

func TestStoreObserveBumpsTheMinuteRing(t *testing.T) {
	base := minuteAt(1_800_000)
	chat := func(after time.Duration, handled bool) observed { return observed{typeChatMessage, after, handled} }
	cases := []struct {
		name      string
		events    []observed
		readAt    time.Duration
		wantNow   int
		wantPrev  int
		wantTicks []int
		wantKey   bool
	}{
		{name: "messages within one minute accumulate and keep the command tick", events: []observed{chat(0, false), chat(10*time.Second, false), chat(20*time.Second, true)}, readAt: 30 * time.Second, wantNow: 3, wantTicks: []int{ringWidth - 1}, wantKey: true},
		{name: "a new minute resets its own bucket and leaves the previous one", events: []observed{chat(0, false), chat(0, false), chat(time.Minute, false)}, readAt: time.Minute, wantNow: 1, wantPrev: 2, wantKey: true},
		{name: "a full lap later reads fresh instead of adding to the old count", events: []observed{chat(0, false), chat(0, false), chat(0, false), chat(ringWidth*time.Minute, false)}, readAt: ringWidth * time.Minute, wantNow: 1, wantKey: true},
		{name: "stream online clears the ring", events: []observed{chat(0, false), {typeStreamOnline, 0, false}}, readAt: 0, wantKey: false},
		{name: "other event types leave the ring untouched", events: []observed{{"channel.follow", 0, false}}, readAt: 0, wantKey: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRing(t, valkeytest.Real(t))
			for _, ev := range tc.events {
				r.store.Observe(chatvolume.Event{BroadcasterID: r.broadcaster, Type: ev.typ, At: base.Add(ev.after), Handled: ev.handled})
			}

			cv := r.read(t, base.Add(tc.readAt))

			assert.Equal(t, tc.wantNow, cv.Now)
			assert.Equal(t, tc.wantPrev, cv.Buckets[ringWidth-2])
			assert.Equal(t, tc.wantTicks, cv.CommandTicks)
			exists, err := r.client.Do(t.Context(), r.client.B().Exists().Key(r.key()).Build()).AsBool()
			require.NoError(t, err)
			assert.Equal(t, tc.wantKey, exists)
		})
	}
}
