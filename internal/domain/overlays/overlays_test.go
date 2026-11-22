// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlays_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/overlays"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return data
}

func codeOf(err error) overlays.Code {
	var failure *overlays.Error
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}

func TestParseValidFixtures(t *testing.T) {
	for _, name := range []string{"valid/system.json", "valid/custom.json"} {
		t.Run(name, func(t *testing.T) {
			_, err := overlays.Parse(fixture(t, name))
			assert.NoError(t, err)
		})
	}
}

func TestParseInvalidFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/invalid/*.json")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		want := strings.TrimSuffix(filepath.Base(file), ".json")
		t.Run(want, func(t *testing.T) {
			_, err := overlays.Parse(fixture(t, filepath.Join("invalid", filepath.Base(file))))
			assert.Equal(t, overlays.Code(want), codeOf(err))
		})
	}
}

func customDocument() overlays.Document {
	return overlays.Document{
		Version: overlays.Version, Kind: overlays.KindCustom, ID: "overlay-1", Name: "Main", Width: 1920, Height: 1080,
	}
}

func widgetWith(id string, kind overlays.WidgetKind, settings map[string]any) overlays.Widget {
	return overlays.Widget{ID: id, Name: "Widget", Kind: kind, W: 100, H: 100, Opacity: 1, Settings: settings}
}

func TestValidateLimits(t *testing.T) {
	tests := []struct {
		name string
		edit func(*overlays.Document)
		want overlays.Code
	}{
		{"widget count", func(d *overlays.Document) {
			for i := 0; i <= overlays.MaxWidgets; i++ {
				d.Widgets = append(d.Widgets, widgetWith("w"+string(rune('a'+i%26))+string(rune('a'+i/26)), overlays.WidgetText, map[string]any{}))
			}
		}, overlays.CodeTooManyWidgets},
		{"asset count", func(d *overlays.Document) {
			d.Assets = map[string]overlays.Asset{}
			for i := 0; i <= overlays.OverlayMaxAssets; i++ {
				d.Assets[strings.Repeat("a", i+1)] = overlays.Asset{}
			}
		}, overlays.CodeTooManyAssets},
		{"document size", func(d *overlays.Document) {
			body := strings.Repeat("a", overlays.DocumentMaxBytes)
			d.Widgets = []overlays.Widget{widgetWith("w1", overlays.WidgetText, map[string]any{"text": body})}
		}, overlays.CodeDocumentTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := customDocument()
			tt.edit(&doc)
			assert.Equal(t, tt.want, codeOf(overlays.Validate(doc)))
		})
	}
}

const cdnURL = "https://cdn.itsbagelbot.com/"

func TestValidateAssetURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want overlays.Code
	}{
		{"cdn url", cdnURL + "u/1/a1", ""},
		{"missing", "", overlays.CodeInvalidAsset},
		{"other host", "https://example.com/u/1/a1", overlays.CodeInvalidAsset},
		{"plain http", "http://cdn.itsbagelbot.com/u/1/a1", overlays.CodeInvalidAsset},
		{"lookalike host", "https://cdn.itsbagelbot.com.example.test/a1", overlays.CodeInvalidAsset},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := customDocument()
			doc.Assets = map[string]overlays.Asset{
				"a1": {Type: overlays.AssetPNG, Bytes: 1024, W: 10, H: 10, SHA256: strings.Repeat("ab", 32), URL: tt.url},
			}
			assert.Equal(t, tt.want, codeOf(overlays.Validate(doc)))
		})
	}
}

func TestMigrate(t *testing.T) {
	const hash = "abababababababababababababababababababababababababababababababab"
	library := map[string]overlays.Asset{
		"a1": {Type: overlays.AssetPNG, Bytes: 1024, W: 400, H: 200, SHA256: hash, URL: cdnURL + "u/1/a1"},
		"a2": {Type: overlays.AssetPNG, Bytes: 2048, W: 400, H: 200, SHA256: strings.Repeat("cd", 32), URL: cdnURL + "u/1/a2"},
	}
	doc, err := overlays.Migrate(fixture(t, "v1.json"), library)
	require.NoError(t, err)
	assert.Equal(
		t,
		[]any{overlays.Version, overlays.KindCustom, "asset:a1", map[string]overlays.Asset{"a1": library["a1"]}},
		[]any{doc.Version, doc.Kind, doc.Widgets[0].Settings["media"], doc.Assets},
	)

	_, err = overlays.Migrate(fixture(t, "v1.json"), nil)
	assert.Equal(t, overlays.CodeUnknownAsset, codeOf(err))

	mp4 := strings.ReplaceAll(string(fixture(t, "v1.json")), hash+".png", hash+".mp4")
	_, err = overlays.Migrate([]byte(mp4), library)
	assert.Equal(t, overlays.CodeUnsupportedMedia, codeOf(err))

	_, err = overlays.Migrate(fixture(t, "valid/custom.json"), library)
	assert.Equal(t, overlays.CodeUnsupportedVersion, codeOf(err))
}

func TestAllowed(t *testing.T) {
	tests := []struct {
		kind    overlays.Kind
		premium bool
		want    bool
	}{
		{overlays.KindSystem, false, true},
		{overlays.KindSystem, true, true},
		{overlays.KindCustom, false, false},
		{overlays.KindCustom, true, true},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, overlays.Allowed(tt.kind, tt.premium), "%s premium=%v", tt.kind, tt.premium)
	}
}

type pinnedLimits struct {
	Limits      map[string]int64            `json:"limits"`
	Assets      map[string]map[string]int64 `json:"assets"`
	WidgetKinds []string                    `json:"widget_kinds"`
}

func currentLimits() pinnedLimits {
	current := pinnedLimits{
		Limits: map[string]int64{
			"document_bytes":           overlays.DocumentMaxBytes,
			"max_widgets":              overlays.MaxWidgets,
			"overlay_asset_bytes":      overlays.OverlayMaxAssetBytes,
			"overlay_assets":           overlays.OverlayMaxAssets,
			"account_bytes":            overlays.AccountMaxBytes,
			"account_overlays":         overlays.AccountMaxOverlays,
			"account_assets":           overlays.AccountMaxAssets,
			"account_uploads_per_hour": overlays.AccountUploadsPerHour,
			"import_unpacked_bytes":    overlays.ImportMaxUnpackedBytes,
			"import_entries":           overlays.ImportMaxEntries,
			"import_ratio":             overlays.ImportMaxRatio,
		},
		Assets: map[string]map[string]int64{},
	}
	for _, kind := range overlays.AssetTypes() {
		limit, _ := overlays.AssetLimitFor(kind)
		current.Assets[string(kind)] = map[string]int64{
			"max_bytes": limit.MaxBytes, "max_width": int64(limit.MaxWidth), "max_height": int64(limit.MaxHeight),
			"max_pixels": limit.MaxPixels, "max_duration_ms": int64(limit.MaxDurationMS),
			"max_frames": int64(limit.MaxFrames), "max_per_overlay": int64(limit.MaxPerOverlay),
		}
	}
	for _, kind := range overlays.WidgetKinds {
		current.WidgetKinds = append(current.WidgetKinds, string(kind))
	}
	return current
}

func TestLimitsMatchPinnedTable(t *testing.T) {
	var pinned pinnedLimits
	require.NoError(t, json.Unmarshal(fixture(t, "limits.json"), &pinned))
	assert.Equal(t, pinned, currentLimits())
}
