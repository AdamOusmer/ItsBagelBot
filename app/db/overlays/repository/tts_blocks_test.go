// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/event/data"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTtsViewerBlocks(t *testing.T) {
	cases := []struct {
		name    string
		blocks  []overlaysrpc.TtsViewerBlock
		unblock string
		wantIDs []string
		wantErr error
	}{
		{"block", []overlaysrpc.TtsViewerBlock{{ViewerID: "42", ViewerLogin: "loud"}}, "", []string{"42"}, nil},
		{"idempotent", []overlaysrpc.TtsViewerBlock{{ViewerID: "42"}, {ViewerID: "042"}}, "", []string{"42"}, nil},
		{"two viewers", []overlaysrpc.TtsViewerBlock{{ViewerID: "9"}, {ViewerID: "42"}}, "", []string{"42", "9"}, nil},
		{"unblock", []overlaysrpc.TtsViewerBlock{{ViewerID: "42"}, {ViewerID: "9"}}, "42", []string{"9"}, nil},
		{"unblock unknown", nil, "77", nil, nil},
		{"login rejected", []overlaysrpc.TtsViewerBlock{{ViewerID: "loud"}}, "", nil, repository.ErrInvalidConfig},
		{"zero rejected", []overlaysrpc.TtsViewerBlock{{ViewerID: "0"}}, "", nil, repository.ErrInvalidConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, repo := setup(t)
			ctx := t.Context()
			var err error
			for _, viewer := range tc.blocks {
				err = repo.BlockTtsViewer(ctx, userID, viewer)
			}
			if tc.unblock != "" {
				err = repo.UnblockTtsViewer(ctx, userID, tc.unblock)
			}
			require.ErrorIs(t, err, tc.wantErr)
			listed, err := repo.TtsBlocks(ctx, userID)
			require.NoError(t, err)
			ids := make([]string, len(listed))
			for i, viewer := range listed {
				ids[i] = viewer.ViewerID
			}
			assert.ElementsMatch(t, tc.wantIDs, ids)
			overlay, err := repo.GetTTS(ctx, userID)
			require.NoError(t, err)
			assert.ElementsMatch(t, tc.wantIDs, overlay.BlockedViewers)
		})
	}
}

func TestTtsBlockChangesReachProjector(t *testing.T) {
	pub, repo := setup(t)
	ctx := t.Context()
	require.NoError(t, repo.BlockTtsViewer(ctx, userID, overlaysrpc.TtsViewerBlock{ViewerID: "42", ViewerLogin: "loud"}))
	require.NoError(t, repo.UnblockTtsViewer(ctx, userID, "42"))

	var got [][]string
	for _, msg := range pub.On(data.SubjectTtsOverlayChanged) {
		var change data.TtsOverlayChangedDTO
		require.NoError(t, codec.Unmarshal(msg.Payload, &change))
		got = append(got, change.Overlay.BlockedViewers)
	}
	assert.Equal(t, [][]string{nil, {"42"}, nil}, got)

	require.NoError(t, repo.BlockTtsViewer(ctx, userID, overlaysrpc.TtsViewerBlock{ViewerID: "42", ViewerLogin: "loud"}))
	listed, err := repo.TtsBlocks(ctx, userID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, "loud", listed[0].ViewerLogin)
}
