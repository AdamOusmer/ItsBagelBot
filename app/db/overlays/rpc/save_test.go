// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"testing"

	"ItsBagelBot/internal/domain/overlays"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func textDocument(t *testing.T, name string) codec.RawMessage {
	t.Helper()
	raw, err := codec.Marshal(overlays.Document{
		Version: overlays.Version, Kind: overlays.KindCustom, ID: "o1", Name: name, Width: 1920, Height: 1080,
		Widgets: []overlays.Widget{{ID: "w1", Name: "Title", Kind: overlays.WidgetText, W: 100, H: 100, Opacity: 1, Settings: map[string]any{"text": "hi"}}},
	})
	require.NoError(t, err)
	return raw
}

func TestSaveWithoutAnIDCreatesACustomOverlay(t *testing.T) {
	nc := serve(t)
	user := overlaysrpc.OverlayUserRequest{UserID: "1001"}
	save := overlaysrpc.OverlaySaveRequest{OverlayRefRequest: overlaysrpc.OverlayRefRequest{OverlayUserRequest: user}, Document: textDocument(t, "New")}

	created := request[overlaysrpc.OverlayView](t, nc, overlaysrpc.OverlaySaveSubject, save)

	require.Equal(t, domainrpc.CodeOK, created.Code, created.Error)
	assert.NotZero(t, created.ID)
	assert.Contains(t, created.URL, overlaysrpc.OverlayURLPrefix)
}

func TestSaveConflictCarriesTheStoredDocument(t *testing.T) {
	nc := serve(t)
	user := overlaysrpc.OverlayUserRequest{UserID: "1001"}
	created := request[overlaysrpc.OverlayView](t, nc, overlaysrpc.OverlayCreateCustomSubject, overlaysrpc.OverlayCreateCustomRequest{OverlayUserRequest: user, Document: textDocument(t, "Mine")})
	stale := overlaysrpc.OverlaySaveRequest{
		OverlayRefRequest: overlaysrpc.OverlayRefRequest{OverlayUserRequest: user, ID: created.ID}, Document: textDocument(t, "Other"), Revision: created.Revision + 4,
	}

	conflict := request[overlaysrpc.OverlayView](t, nc, overlaysrpc.OverlaySaveSubject, stale)

	require.Equal(t, domainrpc.CodeConflict, conflict.Code)
	assert.Equal(t, created.Revision, conflict.Revision)
	assert.JSONEq(t, string(created.Document), string(conflict.Document))
}

func TestStarterTemplatesNeedPremium(t *testing.T) {
	nc := serve(t)
	add := func(userID string) overlaysrpc.OverlayView {
		return request[overlaysrpc.OverlayView](t, nc, overlaysrpc.OverlayAddTemplateSubject,
			overlaysrpc.OverlayAddTemplateRequest{OverlayUserRequest: overlaysrpc.OverlayUserRequest{UserID: userID}, Template: "starting-soon"})
	}

	premium, free := add("1001"), add("1002")

	require.Equal(t, domainrpc.CodeOK, premium.Code, premium.Error)
	assert.Equal(t, "custom", premium.Kind)
	assert.Equal(t, domainrpc.CodeForbidden, free.Code)
}
