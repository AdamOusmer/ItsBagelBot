// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type overlayCredential struct {
	hash       string
	generation int
}

func issueOverlay(t *testing.T, repo *repository.Users) overlayCredential {
	t.Helper()
	ctx := t.Context()
	token, err := repo.RotateOverlayToken(ctx, voiceOwnerID)
	require.NoError(t, err)
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	require.NoError(t, err)
	require.Len(t, raw, 32)

	digest := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(digest[:])
	id, generation, err := repo.ResolveOverlayToken(ctx, hash)
	require.NoError(t, err)
	require.Equal(t, uint64(voiceOwnerID), id)
	return overlayCredential{hash: hash, generation: generation}
}

func TestOverlayTokenRotation(t *testing.T) {
	client, repo := newVoiceAccount(t)
	ctx := t.Context()
	require.NoError(t, repo.SetStatus(ctx, voiceOwnerID, user.StatusVip))
	first := issueOverlay(t, repo)
	second := issueOverlay(t, repo)
	row := client.User.GetX(ctx, voiceOwnerID)
	assert.Equal(t, &second.hash, row.TtsOverlayTokenHash)
	assert.Greater(t, second.generation, first.generation)

	for _, hash := range []string{first.hash, strings.ToUpper(second.hash), second.hash + " ", ""} {
		_, _, err := repo.ResolveOverlayToken(ctx, hash)
		assert.ErrorIs(t, err, repository.ErrOverlayTokenNotFound)
	}
	assert.Equal(t, [2]bool{false, true}, [2]bool{
		repo.OverlayGenerationValid(ctx, voiceOwnerID, first.generation),
		repo.OverlayGenerationValid(ctx, voiceOwnerID, second.generation),
	})
}

func TestOverlayAccountLifecycle(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, *repository.Users)
	}{
		{"inactive", func(t *testing.T, repo *repository.Users) {
			require.NoError(t, repo.SetActiveNow(t.Context(), voiceOwnerID, false))
		}},
		{"banned", func(t *testing.T, repo *repository.Users) {
			require.NoError(t, repo.SetBanned(t.Context(), voiceOwnerID, true))
		}},
		{"erased", func(t *testing.T, repo *repository.Users) {
			require.NoError(t, repo.Delete(t.Context(), voiceOwnerID))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, repo := newVoiceAccount(t)
			ctx := t.Context()
			require.NoError(t, repo.SetStatus(ctx, voiceOwnerID, user.StatusVip))
			issued := issueOverlay(t, repo)

			tc.change(t, repo)
			_, _, err := repo.ResolveOverlayToken(ctx, issued.hash)
			assert.ErrorIs(t, err, repository.ErrOverlayTokenNotFound)
			assert.False(t, repo.OverlayGenerationValid(ctx, voiceOwnerID, issued.generation))
		})
	}
}

func TestOverlayTokenStaysRevokedAfterUnban(t *testing.T) {
	client, repo := newVoiceAccount(t)
	ctx := t.Context()
	require.NoError(t, repo.SetStatus(ctx, voiceOwnerID, user.StatusVip))
	issued := issueOverlay(t, repo)
	require.NoError(t, repo.SetBanned(ctx, voiceOwnerID, true))
	require.NoError(t, repo.SetBanned(ctx, voiceOwnerID, false))
	row := client.User.GetX(ctx, voiceOwnerID)
	assert.Nil(t, row.TtsOverlayTokenHash)
	assert.Greater(t, row.TtsOverlayGeneration, issued.generation)
	assert.False(t, repo.OverlayGenerationValid(ctx, voiceOwnerID, issued.generation))
}

func TestOverlayCookieStaysRevokedAfterReregistration(t *testing.T) {
	_, repo := newVoiceAccount(t)
	ctx := t.Context()
	require.NoError(t, repo.SetStatus(ctx, voiceOwnerID, user.StatusVip))
	issued := issueOverlay(t, repo)
	require.NoError(t, repo.Delete(ctx, voiceOwnerID))

	require.NoError(t, repo.Register(ctx, repository.Registration{
		ID: voiceOwnerID, Username: "voiceowner", Email: "voiceowner@example.invalid",
	}))
	require.NoError(t, repo.SetStatus(ctx, voiceOwnerID, user.StatusVip))
	replacement := issueOverlay(t, repo)
	assert.Greater(t, replacement.generation, issued.generation)
	assert.False(t, repo.OverlayGenerationValid(ctx, voiceOwnerID, issued.generation))
}
