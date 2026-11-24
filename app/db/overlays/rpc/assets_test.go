// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/overlays"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

func foreignAssetDocument(t *testing.T) codec.RawMessage {
	t.Helper()
	doc := overlays.Document{
		Version: overlays.Version, Kind: overlays.KindCustom, ID: "o1", Name: "Mine", Width: 1920, Height: 1080,
		Widgets: []overlays.Widget{{
			ID: "w1", Name: "Logo", Kind: overlays.WidgetImage, W: 100, H: 100, Opacity: 1,
			Settings: map[string]any{"media": "asset:ghost"},
		}},
		Assets: map[string]overlays.Asset{
			"ghost": {Type: overlays.AssetPNG, Bytes: 10, W: 1, H: 1, SHA256: strings.Repeat("a", 64), URL: "https://cdn.itsbagelbot.com/u/1/ghost"},
		},
	}
	raw, err := codec.Marshal(doc)
	require.NoError(t, err)
	return raw
}

func TestAssetRPCGatesOnAlpha(t *testing.T) {
	nc := serve(t)
	document := foreignAssetDocument(t)
	alpha := overlaysrpc.OverlayUserRequest{UserID: "1001"}
	plain := overlaysrpc.OverlayUserRequest{UserID: "1002"}

	cases := []struct {
		name    string
		subject string
		req     any
		want    domainrpc.Code
	}{
		{"plain account asks for an upload", overlaysrpc.AssetBeginSubject, overlaysrpc.AssetBeginRequest{OverlayUserRequest: plain, DeclaredSize: 10}, domainrpc.CodeForbidden},
		{"alpha account asks for an upload", overlaysrpc.AssetBeginSubject, overlaysrpc.AssetBeginRequest{OverlayUserRequest: alpha, DeclaredSize: 10}, domainrpc.CodeOK},
		{"upload with no size", overlaysrpc.AssetBeginSubject, overlaysrpc.AssetBeginRequest{OverlayUserRequest: alpha}, domainrpc.CodeInvalid},
		{"plain account may not dry-run an import", overlaysrpc.ImportBeginSubject, overlaysrpc.ImportBeginRequest{OverlayUserRequest: plain, DeclaredSize: 10, DryRun: true}, domainrpc.CodeForbidden},
		{"alpha account may dry-run an import", overlaysrpc.ImportBeginSubject, overlaysrpc.ImportBeginRequest{OverlayUserRequest: alpha, DeclaredSize: 10, DryRun: true}, domainrpc.CodeOK},
		{"plain account may not import", overlaysrpc.ImportBeginSubject, overlaysrpc.ImportBeginRequest{OverlayUserRequest: plain, DeclaredSize: 10}, domainrpc.CodeForbidden},
		{"plain account lists assets", overlaysrpc.AssetListSubject, plain, domainrpc.CodeForbidden},
		{"alpha account lists assets", overlaysrpc.AssetListSubject, alpha, domainrpc.CodeOK},
		{"plain account deletes an asset", overlaysrpc.AssetDeleteSubject, overlaysrpc.AssetRefRequest{OverlayUserRequest: plain, ID: "ghost"}, domainrpc.CodeForbidden},
		{"plain account saves a custom overlay", overlaysrpc.OverlayCreateCustomSubject, overlaysrpc.OverlayCreateCustomRequest{OverlayUserRequest: plain, Document: document}, domainrpc.CodeForbidden},
		{"alpha account references an asset it does not own", overlaysrpc.OverlayCreateCustomSubject, overlaysrpc.OverlayCreateCustomRequest{OverlayUserRequest: alpha, Document: document}, domainrpc.CodeInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply := request[domainrpc.Refusal](t, nc, tc.subject, tc.req)
			require.Equal(t, tc.want, reply.Code, reply.Error)
		})
	}
}
