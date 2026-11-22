// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlays

import (
	"encoding/json"
	"maps"
	"slices"
)

type legacyDocument struct {
	Version int      `json:"version"`
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Width   int      `json:"width"`
	Height  int      `json:"height"`
	Rev     int64    `json:"rev"`
	Widgets []Widget `json:"widgets"`
}

type migration struct {
	assets map[string]Asset
	byHash map[string]string
	used   map[string]Asset
}

func Migrate(data []byte, assets map[string]Asset) (Document, error) {
	var probe struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(data, &probe) != nil {
		return Document{}, fail(CodeInvalidDocument, "")
	}
	if probe.Version > 1 {
		return Document{}, fail(CodeUnsupportedVersion, "version")
	}
	var old legacyDocument
	if !decodeStrict(data, &old) {
		return Document{}, fail(CodeInvalidDocument, "")
	}
	run := newMigration(assets)
	for i := range old.Widgets {
		if err := run.widget(&old.Widgets[i], Path("widgets").Index(i)); err != nil {
			return Document{}, err
		}
	}
	doc := Document{
		Version: Version, Kind: KindCustom, ID: old.ID, Name: old.Name, Width: old.Width,
		Height: old.Height, Rev: old.Rev, Widgets: old.Widgets, Assets: run.used,
	}
	return doc, Validate(doc)
}

func newMigration(assets map[string]Asset) *migration {
	byHash := make(map[string]string, len(assets))
	for _, id := range slices.Sorted(maps.Keys(assets)) {
		if _, taken := byHash[assets[id].SHA256]; !taken {
			byHash[assets[id].SHA256] = id
		}
	}
	return &migration{assets: assets, byHash: byHash, used: make(map[string]Asset)}
}

func (m *migration) widget(widget *Widget, path Path) error {
	if widget.Settings == nil {
		return nil
	}
	rewritten, code := mapStrings(widget.Settings, m.rewrite)
	if code != "" {
		return fail(code, path.Field("settings"))
	}
	widget.Settings = rewritten.(map[string]any)
	return nil
}

func (m *migration) rewrite(text string) (string, Code) {
	match := legacyMedia.FindStringSubmatch(text)
	if match == nil {
		return text, ""
	}
	if match[2] == "mp4" {
		return text, CodeUnsupportedMedia
	}
	id, found := m.byHash[match[1]]
	if !found {
		return text, CodeUnknownAsset
	}
	m.used[id] = m.assets[id]
	return assetPrefix + id, ""
}
