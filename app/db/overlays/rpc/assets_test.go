// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"encoding/json"
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/overlays"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"

	"github.com/stretchr/testify/require"
)

func foreignAssetDocument(t *testing.T) json.RawMessage {
	t.Helper()
	doc := overlays.Document{
		Version: overlays.Version, Kind: overlays.KindCustom, ID: "o1", Name: "Mine", Width: 1920, Height: 1080,
		Widgets: []overlays.Widget{{
			ID: "w1", Name: "Logo", Kind: overlays.WidgetImage, W: 100, H: 100, Opacity: 1,
			Settings: map[string]any{"media": "asset:ghost"},
		}},
		Assets: map[string]overlays.Asset{
			"ghost": {Type: overlays.AssetPNG, Bytes: 10, W: 1, H: 1, SHA256: strings.Repeat("a", 64)},
		},
	}
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	return raw
}

func TestAssetRPCGatesOnPremium(t *testing.T) {
	nc := serve(t)
	document := foreignAssetDocument(t)
	premium := overlaysrpc.OverlayUserRequest{UserID: "1001"}
	free := overlaysrpc.OverlayUserRequest{UserID: "1002"}

	cases := []struct {
		name    string
		subject string
		req     any
		want    domainrpc.Code
	}{
		{"free account asks for an upload", overlaysrpc.AssetBeginSubject, overlaysrpc.AssetBeginRequest{OverlayUserRequest: free, DeclaredSize: 10}, domainrpc.CodeForbidden},
		{"premium account asks for an upload", overlaysrpc.AssetBeginSubject, overlaysrpc.AssetBeginRequest{OverlayUserRequest: premium, DeclaredSize: 10}, domainrpc.CodeOK},
		{"upload with no size", overlaysrpc.AssetBeginSubject, overlaysrpc.AssetBeginRequest{OverlayUserRequest: premium}, domainrpc.CodeInvalid},
		{"free account may dry-run an import", overlaysrpc.ImportBeginSubject, overlaysrpc.ImportBeginRequest{OverlayUserRequest: free, DeclaredSize: 10, DryRun: true}, domainrpc.CodeOK},
		{"free account may not import", overlaysrpc.ImportBeginSubject, overlaysrpc.ImportBeginRequest{OverlayUserRequest: free, DeclaredSize: 10}, domainrpc.CodeForbidden},
		{"free account saves a custom overlay", overlaysrpc.OverlayCreateCustomSubject, overlaysrpc.OverlayCreateCustomRequest{OverlayUserRequest: free, Document: document}, domainrpc.CodeForbidden},
		{"premium account references an asset it does not own", overlaysrpc.OverlayCreateCustomSubject, overlaysrpc.OverlayCreateCustomRequest{OverlayUserRequest: premium, Document: document}, domainrpc.CodeInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply := request[domainrpc.Refusal](t, nc, tc.subject, tc.req)
			require.Equal(t, tc.want, reply.Code, reply.Error)
		})
	}
}
