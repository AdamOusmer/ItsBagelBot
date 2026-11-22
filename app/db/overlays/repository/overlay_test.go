// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/overlays"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const otherUserID = 1002

func overlayChanges(t *testing.T, pub *bustest.Publisher) []data.OverlayChangedDTO {
	t.Helper()
	var out []data.OverlayChangedDTO
	for _, msg := range pub.On(data.SubjectOverlayChanged) {
		var dto data.OverlayChangedDTO
		require.NoError(t, codec.Unmarshal(msg.Payload, &dto))
		out = append(out, dto)
	}
	return out
}

func configOf(t *testing.T, overlay overlaysrpc.Overlay) overlaysrpc.PoweredConfig {
	t.Helper()
	doc, err := overlays.Parse(overlay.Document)
	require.NoError(t, err)
	require.NotNil(t, doc.Config)
	return *doc.Config
}

func TestMigratePoweredKeepsTheExistingLink(t *testing.T) {
	pub, repo := setup(t)
	ctx := t.Context()
	legacy, err := repo.Get(ctx, userID)
	require.NoError(t, err)
	cfg := overlaysrpc.PoweredConfig{EveryMinutes: 30, ShowSeconds: 10, Directions: []overlaysrpc.Direction{overlaysrpc.DirectionLeft}, Align: overlaysrpc.AlignStart}
	_, err = repo.Save(ctx, userID, cfg, legacy.Revision)
	require.NoError(t, err)

	require.NoError(t, repo.MigratePowered(ctx))
	require.NoError(t, repo.MigratePowered(ctx))

	list, err := repo.ListOverlays(ctx, userID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	found, err := repo.OverlayByHash(ctx, overlaysrpc.HashToken(legacy.Token))
	require.NoError(t, err)
	assert.Equal(t, list[0], found)
	assert.Equal(t, [3]string{"system", "powered", "Powered by Bagel"}, [3]string{found.Kind, found.Template, found.Name})
	assert.Equal(t, cfg, configOf(t, found))
	assert.Empty(t, overlayChanges(t, pub), "the boot migration does not announce")
}

func TestAddTemplateIsOnePerUserAndTemplate(t *testing.T) {
	pub, repo := setup(t)
	ctx := t.Context()

	first, token, err := repo.AddTemplate(ctx, userID, "powered")
	require.NoError(t, err)
	again, againToken, err := repo.AddTemplate(ctx, userID, "powered")
	require.NoError(t, err)
	_, _, unknown := repo.AddTemplate(ctx, userID, "missing")

	assert.Equal(t, first, again)
	assert.Equal(t, [2]bool{true, true}, [2]bool{token != "", againToken == ""})
	require.ErrorIs(t, unknown, repository.ErrUnknownTemplate)
	assert.Equal(t, overlaysrpc.HashToken(token), first.TokenHash)
	assert.Equal(t, []data.OverlayChangedDTO{{Overlay: first}}, overlayChanges(t, pub))
}

func TestRotateChangesTokenAndGeneration(t *testing.T) {
	pub, repo := setup(t)
	ctx := t.Context()
	created, oldToken, err := repo.AddTemplate(ctx, userID, "powered")
	require.NoError(t, err)

	rotated, newToken, err := repo.RotateOverlay(ctx, userID, created.ID)
	require.NoError(t, err)

	assert.NotEqual(t, oldToken, newToken)
	assert.Equal(t, overlaysrpc.HashToken(newToken), rotated.TokenHash)
	assert.Greater(t, rotated.Generation, created.Generation)
	assert.Equal(t, created.Revision+1, rotated.Revision)
	_, err = repo.OverlayByHash(ctx, overlaysrpc.HashToken(oldToken))
	require.ErrorIs(t, err, repository.ErrNotFound)
	events := overlayChanges(t, pub)
	require.Len(t, events, 2)
	assert.Equal(t, data.OverlayChangedDTO{Overlay: rotated, PreviousTokenHash: created.TokenHash}, events[1])
}

func TestSaveOverlayChecksConfigAndRevision(t *testing.T) {
	fresh := func(t *testing.T) (*repository.Overlays, overlaysrpc.Overlay) {
		_, r := setup(t)
		created, _, err := r.AddTemplate(t.Context(), userID, "powered")
		require.NoError(t, err)
		return r, created
	}
	valid := overlaysrpc.PoweredConfig{EveryMinutes: 45, ShowSeconds: 12, Directions: []overlaysrpc.Direction{overlaysrpc.DirectionTop}, Align: overlaysrpc.AlignCenter}
	invalid := valid
	invalid.EveryMinutes = 1
	for _, tc := range []struct {
		name     string
		cfg      overlaysrpc.PoweredConfig
		revision uint64
		want     error
	}{
		{"saved", valid, 1, nil},
		{"stale revision", valid, 7, repository.ErrRevisionConflict},
		{"invalid config", invalid, 1, repository.ErrInvalidConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, created := fresh(t)
			req := overlaysrpc.OverlaySaveRequest{OverlayRefRequest: overlaysrpc.OverlayRefRequest{ID: created.ID}, Config: tc.cfg, Revision: tc.revision}
			saved, err := r.SaveOverlay(t.Context(), userID, req)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.cfg, configOf(t, saved))
		})
	}
}

func TestDeleteOverlayIsScopedToTheOwner(t *testing.T) {
	pub, repo := setup(t)
	ctx := t.Context()
	created, _, err := repo.AddTemplate(ctx, userID, "powered")
	require.NoError(t, err)

	require.ErrorIs(t, repo.DeleteOverlay(ctx, otherUserID, created.ID), repository.ErrNotFound)
	require.NoError(t, repo.DeleteOverlay(ctx, userID, created.ID))

	_, err = repo.GetOverlay(ctx, userID, created.ID)
	require.ErrorIs(t, err, repository.ErrNotFound)
	events := overlayChanges(t, pub)
	require.Len(t, events, 2)
	assert.Equal(t, data.OverlayChangedDTO{Overlay: created, Deleted: true}, events[1])
}

func TestDeleteAccountHonorsTheCutoffForOverlays(t *testing.T) {
	_, repo := setup(t)
	ctx := t.Context()
	created, _, err := repo.AddTemplate(ctx, userID, "powered")
	require.NoError(t, err)
	createdAt := time.UnixMicro(created.CreatedAt)

	require.NoError(t, repo.DeleteAccount(ctx, userID, createdAt.Add(-time.Nanosecond)))
	_, err = repo.GetOverlay(ctx, userID, created.ID)
	require.NoError(t, err)

	require.NoError(t, repo.DeleteAccount(ctx, userID, createdAt))
	_, err = repo.GetOverlay(ctx, userID, created.ID)
	require.ErrorIs(t, err, repository.ErrNotFound)
}
