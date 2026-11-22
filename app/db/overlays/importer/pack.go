// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package importer

import (
	"bytes"
	"encoding/json"
	"path"
	"strings"

	"ItsBagelBot/internal/domain/overlays"
)

const manifestFormat = 1

type manifest struct {
	Format   int             `json:"format"`
	Name     string          `json:"name"`
	Author   string          `json:"author"`
	Version  string          `json:"version"`
	License  string          `json:"license"`
	Overlays []string        `json:"overlays"`
	Assets   []manifestAsset `json:"assets"`
}

type manifestAsset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func (m manifest) valid() bool {
	return m.Format == manifestFormat && strings.TrimSpace(m.Name) != "" && len(m.Overlays) > 0
}

func (b *builder) sellerPack(root entry) (Plan, error) {
	b.seller = true
	man, err := b.readManifest(root)
	if err != nil {
		return Plan{}, err
	}
	dir := path.Dir(root.Path)
	ids, err := b.registerManifestAssets(dir, man.Assets)
	if err != nil {
		return Plan{}, err
	}
	docs, err := b.readPackOverlays(dir, man.Overlays, ids)
	if err != nil {
		return Plan{}, err
	}
	b.leftovers()
	meta := Meta{Name: man.Name, Author: man.Author, Version: man.Version, License: man.License}
	return Plan{Document: docs[0], Others: docs[1:], Files: b.files, Report: b.report, Pack: &meta}, nil
}

func (b *builder) readManifest(root entry) (manifest, error) {
	b.used[root.Path] = true
	data, within, err := root.read(overlays.DocumentMaxBytes)
	if err != nil {
		return manifest{}, err
	}
	var man manifest
	decoded := within && decodeStrict(data, &man)
	if !decoded || !man.valid() {
		return manifest{}, &Error{Code: CodeInvalidManifest, Path: root.Path}
	}
	return man, nil
}

func decodeStrict(data []byte, into any) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(into) == nil && !decoder.More()
}

func (b *builder) registerManifestAssets(dir string, assets []manifestAsset) (map[string]string, error) {
	ids := make(map[string]string, len(assets))
	for _, asset := range assets {
		e, ok := b.take(dir, asset.Path)
		if !ok {
			return nil, &Error{Code: CodeMissingAsset, Path: asset.Path}
		}
		if !strings.EqualFold(e.SHA256, asset.SHA256) {
			return nil, &Error{Code: CodeBadHash, Path: asset.Path}
		}
		if id := b.registerEntry(e); id != "" {
			ids[e.SHA256] = id
		}
	}
	return ids, nil
}

func (b *builder) readPackOverlays(dir string, names []string, ids map[string]string) ([]overlays.Document, error) {
	docs := make([]overlays.Document, 0, len(names))
	for _, name := range names {
		doc, err := b.readPackOverlay(dir, name, ids)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func (b *builder) readPackOverlay(dir, name string, ids map[string]string) (overlays.Document, error) {
	e, ok := b.take(dir, name)
	if !ok {
		return overlays.Document{}, &Error{Code: CodeInvalidManifest, Path: name}
	}
	data, within, err := e.read(overlays.DocumentMaxBytes)
	if err != nil {
		return overlays.Document{}, err
	}
	doc, err := overlays.Parse(withPlaceholderURLs(data))
	if !within || err != nil {
		return overlays.Document{}, &Error{Code: CodeInvalidDocument, Path: e.Path}
	}
	b.report.mapped(Subject(e.Path))
	return rekey(doc, ids)
}

// Sellers never know our CDN addresses; the caller replaces these placeholders with the stored object URLs.
const placeholderURL = "https://cdn.itsbagelbot.com/pending"

func withPlaceholderURLs(data []byte) []byte {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var tree map[string]any
	if decoder.Decode(&tree) != nil {
		return data
	}
	assets, _ := tree["assets"].(map[string]any)
	for _, entry := range assets {
		if asset, ok := entry.(map[string]any); ok {
			asset["url"] = placeholderURL
		}
	}
	out, err := json.Marshal(tree)
	if err != nil {
		return data
	}
	return out
}

func rekey(doc overlays.Document, ids map[string]string) (overlays.Document, error) {
	rename := make(map[string]string, len(doc.Assets))
	assets := make(map[string]overlays.Asset, len(doc.Assets))
	for old, asset := range doc.Assets {
		id, ok := ids[asset.SHA256]
		if !ok {
			return overlays.Document{}, &Error{Code: CodeMissingAsset, Path: old}
		}
		rename[old] = id
		assets[id] = asset
	}
	for i := range doc.Widgets {
		doc.Widgets[i].Settings = mapStrings(doc.Widgets[i].Settings, renamer(rename)).(map[string]any)
	}
	doc.Assets = assets
	return doc, nil
}

func renamer(rename map[string]string) func(string) string {
	return func(text string) string {
		old, isRef := strings.CutPrefix(text, assetPrefix)
		if id, found := rename[old]; isRef && found {
			return assetPrefix + id
		}
		return text
	}
}

func (b *builder) leftovers() {
	for _, e := range b.entries {
		if b.used[e.Path] {
			continue
		}
		reason := ruleFor(e.Path).reason
		if reason == "" {
			reason = ReasonNotInManifest
		}
		b.report.unsupported(Subject(e.Path), reason)
	}
}
