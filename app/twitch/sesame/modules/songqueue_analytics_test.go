// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"
	pkgvalkey "ItsBagelBot/pkg/valkey"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSongRequestRecordsOutcomes(t *testing.T) {
	for _, tc := range []struct {
		request        srCase
		outcome        string
		trial          bool
		missingChatter bool
		addErr         error
	}{
		{request: srCase{}, outcome: "queued"},
		{request: srCase{noGossip: true}},
		{missingChatter: true},
		{request: srCase{config: `{"sr":{"enabled":false}}`}, outcome: "off"},
		{request: srCase{config: `{"sr":{"perm":"mod"}}`}, outcome: "perm"},
		{request: srCase{config: `{"sr":{"allowOffline":false}}`, live: liveOffline}, outcome: "offline"},
		{request: srCase{noTracks: true}, outcome: "not_found"},
		{request: srCase{config: `{"maxDepth":1}`, store: fakeSongQueue{up: []engine.SongEntry{{TrackID: "existing"}}}}, outcome: "queue_full"},
		{request: srCase{drop: "spotify.queue"}, outcome: "player_refused"},
		{addErr: fmt.Errorf("wrapped: %w", engine.ErrSongQuotaReached), outcome: "queue_full"},
		{addErr: fmt.Errorf("wrapped: %w", engine.ErrSongQueueFull), outcome: "queue_full"},
		{addErr: errors.New("store unavailable")},
		{request: srCase{}, trial: true},
	} {
		t.Run(tc.outcome+tc.request.config, func(t *testing.T) {
			vc := valkeytest.Spawn(t)
			rec := analytics.New(vc, zap.NewNop(), nil, analytics.WithoutTicker())
			store := tc.request.store
			d := tc.request.deps(&store, tc.request.gossip())
			if tc.addErr != nil {
				d.SongQueue = songAddFailure{SongQueueStore: &store, err: tc.addErr}
			}
			d.Analytics = rec
			c := withConfig(chatCtx("42", "alice"), tc.request.config)
			if tc.missingChatter {
				c.Env.ChatterUserID = ""
			}
			if tc.trial {
				c.Env.Origin = "trial"
			}
			_, runErr := runChatErr(t, SongQueue(d), c, "!sr Human")
			require.Equal(t, tc.addErr != nil && tc.outcome == "", runErr != nil)
			rec.Close()
			counts, err := pkgvalkey.HashAllInts(context.Background(), vc, catalog.CounterKey(time.Now()))
			require.NoError(t, err)
			want := map[string]int64{}
			if tc.outcome != "" {
				want["songs.requests|"+tc.outcome] = 1
			}
			require.Equal(t, want, counts)
		})
	}
}

type songAddFailure struct {
	engine.SongQueueStore
	err error
}

func (s songAddFailure) Add(context.Context, uint64, engine.SongEntry, engine.SongQueueLimits) (int, error) {
	return 0, s.err
}
