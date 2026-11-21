// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection_test

import (
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTtsOverlayFoldFencesOldHashesAndIncarnations(t *testing.T) {
	store := projection.NewStore(valkeytest.Spawn(t))
	ctx := t.Context()
	first := overlaysrpc.TtsOverlay{UserID: 43, Revision: 1, CreatedAt: 100, Generation: 10,
		Settings: overlaysrpc.DefaultTtsSettings(), TokenHash: overlaysrpc.TtsTokenHash(strings.Repeat("a", 64))}
	rotated := first
	rotated.Revision, rotated.Generation, rotated.TokenHash = 2, 11, overlaysrpc.TtsTokenHash(strings.Repeat("b", 64))
	recreated := rotated
	recreated.CreatedAt, recreated.Revision = 200, 1
	cases := []struct {
		name             string
		row, want        overlaysrpc.TtsOverlay
		deleted, applied bool
	}{
		{"create", first, first, false, true},
		{"rotate", rotated, rotated, false, true},
		{"late save", first, rotated, false, false},
		{"late delete", first, rotated, true, false},
		{"delete", rotated, overlaysrpc.TtsOverlay{}, true, true},
		{"stale cold read", rotated, overlaysrpc.TtsOverlay{}, false, false},
		{"re-register", recreated, recreated, false, true},
		{"late old account delete", rotated, recreated, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			applied, err := store.FoldTtsOverlay(ctx, data.TtsOverlayChangedDTO{Overlay: tc.row, Deleted: tc.deleted})
			require.NoError(t, err)
			assert.Equal(t, tc.applied, applied)
			assertTtsIndexes(t, store, tc.want, [2]overlaysrpc.TtsTokenHash{first.TokenHash, rotated.TokenHash})
		})
	}
}

func assertTtsIndexes(t *testing.T, store *projection.Store, want overlaysrpc.TtsOverlay, hashes [2]overlaysrpc.TtsTokenHash) {
	t.Helper()
	for _, hash := range hashes {
		row, found, err := store.TtsOverlayByHash(t.Context(), hash)
		require.NoError(t, err)
		require.Equal(t, want.TokenHash == hash, found)
		if found {
			assert.Equal(t, want, row)
		}
	}
}
