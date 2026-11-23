// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/event/data"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/domain/voiceplan"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTtsDefaultsAndSave(t *testing.T) {
	cases := []struct {
		name     string
		settings overlaysrpc.TtsSettings
		revision uint64
		wantErr  error
	}{
		{"valid", overlaysrpc.TtsSettings{Enabled: false, Voice: "Puck", MaxSeconds: 45}, 1, nil},
		{"stale", overlaysrpc.DefaultTtsSettings(), 9, repository.ErrRevisionConflict},
		{"invalid length", overlaysrpc.TtsSettings{MaxSeconds: 4}, 1, repository.ErrInvalidConfig},
		{"invalid voice", overlaysrpc.TtsSettings{Voice: "puck", MaxSeconds: 5}, 1, repository.ErrInvalidConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pub, repo := setup(t)
			initial, err := repo.GetTTS(t.Context(), userID)
			require.NoError(t, err)
			assert.Equal(t, overlaysrpc.DefaultTtsSettings(), initial.Settings)
			assert.False(t, initial.Configured())
			saved, err := repo.SaveTTS(t.Context(), userID, overlaysrpc.TtsSaveRequest{Settings: tc.settings, Revision: tc.revision})
			require.ErrorIs(t, err, tc.wantErr)
			if tc.wantErr != nil {
				require.Len(t, pub.On(data.SubjectTtsOverlayChanged), 1)
				return
			}
			assert.Equal(t, tc.settings, saved.Settings)
			assert.Equal(t, initial.Revision+1, saved.Revision)
			require.Len(t, pub.On(data.SubjectTtsOverlayChanged), 2)
		})
	}
}

func TestTtsTokenRotationAndAccountBan(t *testing.T) {
	_, repo := setup(t)
	ctx := t.Context()
	_, _, err := repo.RotateTTS(ctx, userID, voiceplan.Free)
	require.ErrorIs(t, err, repository.ErrTtsPlan)

	first, _, err := repo.RotateTTS(ctx, userID, voiceplan.AdminGrant)
	require.NoError(t, err)
	second, token, err := repo.RotateTTS(ctx, userID, voiceplan.AdminGrant)
	require.NoError(t, err)
	assert.Equal(t, overlaysrpc.TtsTokenHash(fmt.Sprintf("%x", sha256.Sum256([]byte(token)))), second.TokenHash)
	assert.Greater(t, second.Generation, first.Generation)

	_, err = repo.TTSByHash(ctx, first.TokenHash)
	require.ErrorIs(t, err, repository.ErrNotFound)
	bannedAt := time.UnixMicro(int64(second.Generation)).In(time.FixedZone("UTC-05", -5*60*60))
	require.NoError(t, repo.RevokeBannedTTS(ctx, data.UserBannedDTO{UserID: userID, BannedAt: bannedAt}))
	_, err = repo.TTSByHash(ctx, second.TokenHash)
	require.ErrorIs(t, err, repository.ErrNotFound)
}

func TestTtsDeletionHonorsTheAccountCutoff(t *testing.T) {
	pub, repo := setup(t)
	ctx := t.Context()
	initial, err := repo.GetTTS(ctx, userID)
	require.NoError(t, err)
	linked, _, err := repo.RotateTTS(ctx, userID, voiceplan.AdminGrant)
	require.NoError(t, err)
	created := time.UnixMicro(initial.CreatedAt).In(time.FixedZone("UTC-05", -5*60*60))

	require.NoError(t, repo.DeleteAccount(ctx, userID, created.Add(-time.Nanosecond)))
	read, err := repo.TTSByHash(ctx, linked.TokenHash)
	require.NoError(t, err)
	assert.Equal(t, linked, read)

	require.NoError(t, repo.DeleteAccount(ctx, userID, created))
	_, err = repo.TTSByHash(ctx, linked.TokenHash)
	require.ErrorIs(t, err, repository.ErrNotFound)
	require.Len(t, pub.On(data.SubjectTtsOverlayChanged), 3)
}
