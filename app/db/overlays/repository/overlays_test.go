// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/overlays/ent"
	"ItsBagelBot/app/db/overlays/ent/enttest"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/event/data"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/objectstore/objectstoretest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

const userID = 1001

func setup(t *testing.T) (*bustest.Publisher, *repository.Overlays) {
	t.Helper()
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	pub := bustest.NewPublisher()
	return pub, repository.New(client, pub, objectstoretest.NewMemory(), "https://cdn.itsbagelbot.com")
}

func changes(t *testing.T, pub *bustest.Publisher) []data.PoweredOverlayChangedDTO {
	t.Helper()
	var out []data.PoweredOverlayChangedDTO
	for _, msg := range pub.On(data.SubjectPoweredOverlayChanged) {
		var dto data.PoweredOverlayChangedDTO
		require.NoError(t, codec.Unmarshal(msg.Payload, &dto))
		out = append(out, dto)
	}
	return out
}

func TestGetCreatesTheDefaultOverlayOnce(t *testing.T) {
	pub, repo := setup(t)

	first, err := repo.Get(t.Context(), userID)
	require.NoError(t, err)
	again, err := repo.Get(t.Context(), userID)
	require.NoError(t, err)

	want := overlaysrpc.PoweredOverlay{UserID: userID, Token: first.Token, Config: overlaysrpc.DefaultPoweredConfig(), Revision: 1}
	require.Equal(t, [2]overlaysrpc.PoweredOverlay{want, want}, [2]overlaysrpc.PoweredOverlay{first, again})
	require.Len(t, first.Token, 43)
	require.Equal(t, []data.PoweredOverlayChangedDTO{{UserID: userID, Overlay: &want}}, changes(t, pub))
}

func TestSaveChecksConfigAndRevision(t *testing.T) {
	valid := overlaysrpc.PoweredConfig{EveryMinutes: 30, ShowSeconds: 10, Directions: []overlaysrpc.Direction{overlaysrpc.DirectionLeft}, Align: overlaysrpc.AlignStart}
	invalid := valid
	invalid.ShowSeconds = 99

	cases := []struct {
		name     string
		config   overlaysrpc.PoweredConfig
		revision uint64
		wantErr  error
		want     overlaysrpc.PoweredConfig
		wantRev  uint64
	}{
		{"invalid config", invalid, 1, repository.ErrInvalidConfig, overlaysrpc.DefaultPoweredConfig(), 1},
		{"stale revision", valid, 7, repository.ErrRevisionConflict, overlaysrpc.DefaultPoweredConfig(), 1},
		{"saved", valid, 1, nil, valid, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, repo := setup(t)
			_, err := repo.Get(t.Context(), userID)
			require.NoError(t, err)

			_, err = repo.Save(t.Context(), userID, tc.config, tc.revision)
			require.ErrorIs(t, err, tc.wantErr)

			stored, err := repo.Find(t.Context(), userID)
			require.NoError(t, err)
			require.Equal(t, [2]any{tc.want, tc.wantRev}, [2]any{stored.Config, stored.Revision})
		})
	}
}

func TestRotateRetiresThePreviousToken(t *testing.T) {
	pub, repo := setup(t)
	before, err := repo.Get(t.Context(), userID)
	require.NoError(t, err)

	rotated, err := repo.Rotate(t.Context(), userID)
	require.NoError(t, err)
	require.NotEqual(t, before.Token, rotated.Token)

	_, err = repo.ByToken(t.Context(), before.Token)
	require.ErrorIs(t, err, repository.ErrNotFound)
	found, err := repo.ByToken(t.Context(), rotated.Token)
	require.NoError(t, err)

	events := changes(t, pub)
	require.Equal(t, data.PoweredOverlayChangedDTO{UserID: userID, PreviousToken: before.Token, Overlay: &found}, events[len(events)-1])
}

func TestByTokenMissesUnknownTokens(t *testing.T) {
	_, repo := setup(t)
	_, err := repo.Get(t.Context(), userID)
	require.NoError(t, err)

	for _, token := range []string{"", "unknown", repository.NewToken()} {
		_, err := repo.ByToken(t.Context(), token)
		require.ErrorIs(t, err, repository.ErrNotFound, token)
	}
}

func TestDeleteAccountKeepsRowsOfAReregisteredAccount(t *testing.T) {
	cases := []struct {
		name    string
		cutoff  time.Duration
		deleted bool
	}{
		{"row older than the deletion", time.Minute, true},
		{"row created after the deletion", -time.Minute, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pub, repo := setup(t)
			created, err := repo.Get(t.Context(), userID)
			require.NoError(t, err)

			require.NoError(t, repo.DeleteAccount(t.Context(), userID, time.Now().Add(tc.cutoff)))

			want := data.PoweredOverlayChangedDTO{UserID: userID, Overlay: &created}
			if tc.deleted {
				want = data.PoweredOverlayChangedDTO{UserID: userID, PreviousToken: created.Token, Deleted: true, Overlay: &created}
			}
			_, err = repo.Find(t.Context(), userID)
			require.Equal(t, tc.deleted, err != nil)
			events := changes(t, pub)
			require.Equal(t, want, events[len(events)-1])
		})
	}
}
