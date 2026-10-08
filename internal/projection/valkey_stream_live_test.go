// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	livekey "ItsBagelBot/internal/domain/live"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

type streamLiveFixture struct {
	t      *testing.T
	ctx    context.Context
	store  *Store
	client valkey.Client
	user   uint64
}

func newStreamLiveFixture(t *testing.T) streamLiveFixture {
	t.Helper()
	address := os.Getenv("VALKEY_TEST_ADDR")
	if address == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:  []string{address},
		Password:     os.Getenv("VALKEY_TEST_PASSWORD"),
		DisableCache: true,
	})
	require.NoError(t, err)
	f := streamLiveFixture{t: t, ctx: context.Background(), store: NewStore(client), client: client, user: uint64(time.Now().UnixNano())}
	t.Cleanup(func() {
		client.Do(f.ctx, client.B().Del().Key(f.settingsKey(), livekey.Key(f.user), livekey.VerKey(f.user)).Build())
		client.Close()
	})
	return f
}

func (f streamLiveFixture) settingsKey() string {
	return settingsKeyPrefix + strconv.FormatUint(f.user, 10)
}

func (f streamLiveFixture) fold(live bool, version int64) StreamLiveFold {
	f.t.Helper()
	got, err := f.store.SetStreamLive(f.ctx, f.user, StreamLive{Live: live, Version: version})
	require.NoError(f.t, err)
	return got
}

func (f streamLiveFixture) twitchVerdict(script string, version int64) {
	f.t.Helper()
	err := f.client.Do(f.ctx, f.client.B().Eval().Script(script).Numkeys(2).
		Key(livekey.Key(f.user), livekey.VerKey(f.user)).
		Arg(livekey.Value(version), "60", "120").Build()).Error()
	require.NoError(f.t, err)
}

func (f streamLiveFixture) read() StreamLive {
	f.t.Helper()
	got, err := f.store.GetStreamLive(f.ctx, f.user)
	require.NoError(f.t, err)
	return got
}

func TestStreamLiveFoldRefusesStaleEvents(t *testing.T) {
	f := newStreamLiveFixture(t)

	assert.Equal(t, StreamLiveFold{Applied: true}, f.fold(false, 2000))
	assert.Equal(t, StreamLiveFold{}, f.fold(true, 1000), "an online older than the applied offline must lose")

	assert.Equal(t, StreamLive{Known: true, Version: 2000}, f.read())
}

func TestStreamLiveFoldReportsThePriorState(t *testing.T) {
	f := newStreamLiveFixture(t)

	assert.Equal(t, StreamLiveFold{Applied: true}, f.fold(true, 1000), "first online is the go-live edge")
	assert.Equal(t, StreamLiveFold{Applied: true, WasLive: true}, f.fold(true, 1000), "a redelivered online is not an edge")
	assert.Equal(t, StreamLiveFold{Applied: true, WasLive: true}, f.fold(false, 2000))
	assert.Equal(t, StreamLiveFold{Applied: true}, f.fold(true, 3000), "the next stream is an edge again")

	ttl, err := f.client.Do(f.ctx, f.client.B().Ttl().Key(f.settingsKey()).Build()).AsInt64()
	require.NoError(t, err)
	assert.Positive(t, ttl)
}

func TestStreamLiveReadTakesTheNewestVerdict(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f streamLiveFixture)
		want  StreamLive
	}{
		{
			name:  "nothing recorded",
			setup: func(streamLiveFixture) {},
			want:  StreamLive{},
		},
		{
			name: "Twitch restored a stream the projection still holds offline",
			setup: func(f streamLiveFixture) {
				f.fold(false, 2000)
				f.twitchVerdict(livekey.SetScript, 3000)
			},
			want: StreamLive{Live: true, Known: true, Version: 3000},
		},
		{
			name: "a newer clear outranks a projected live",
			setup: func(f streamLiveFixture) {
				f.fold(true, 1000)
				f.twitchVerdict(livekey.ClearScript, 2000)
			},
			want: StreamLive{Known: true, Version: 2000},
		},
		{
			name: "a projected live outranks an older clear",
			setup: func(f streamLiveFixture) {
				f.twitchVerdict(livekey.ClearScript, 1000)
				f.fold(true, 2000)
			},
			want: StreamLive{Live: true, Known: true, Version: 2000},
		},
		{
			name: "only the live key is known",
			setup: func(f streamLiveFixture) {
				f.twitchVerdict(livekey.ClearScript, 1000)
			},
			want: StreamLive{Known: true, Version: 1000},
		},
		{
			name: "an unversioned projected live from before the upgrade",
			setup: func(f streamLiveFixture) {
				require.NoError(f.t, f.client.Do(f.ctx, f.client.B().Hset().Key(f.settingsKey()).FieldValue().FieldValue("live", "1").Build()).Error())
			},
			want: StreamLive{Live: true, Known: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStreamLiveFixture(t)
			tt.setup(f)
			assert.Equal(t, tt.want, f.read())
		})
	}
}
