// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package importer

import (
	"io"
	"path"
	"strconv"
	"strings"

	"ItsBagelBot/internal/domain/overlays"
)

const (
	defaultName   = "Imported overlay"
	defaultWidth  = 1920
	defaultHeight = 1080
	assetPrefix   = "asset:"
	codeLimit     = 512 << 10
	exportLimit   = 4 << 20
	manifestName  = "overlay-pack.json"
)

type Subject string

type Item struct {
	Subject Subject
	Reason  Reason
}

type Report struct {
	Mapped       []Item
	Approximated []Item
	Unsupported  []Item
}

func (r *Report) mapped(subject Subject) {
	r.Mapped = append(r.Mapped, Item{Subject: subject})
}

func (r *Report) approximated(subject Subject, reason Reason) {
	r.Approximated = append(r.Approximated, Item{Subject: subject, Reason: reason})
}

func (r *Report) unsupported(subject Subject, reason Reason) {
	r.Unsupported = append(r.Unsupported, Item{Subject: subject, Reason: reason})
}

type File struct {
	ID     string
	Path   string
	Group  string
	Type   overlays.AssetType
	SHA256 string
	Size   int64
	Open   func() (io.ReadCloser, error)
}

type Meta struct {
	Name    string
	Author  string
	Version string
	License string
}

type Plan struct {
	Document overlays.Document
	Others   []overlays.Document
	Files    []File
	Report   Report
	Pack     *Meta
}

type builder struct {
	entries []entry
	index   map[string]entry
	used    map[string]bool
	files   []File
	byPath  map[string]string
	byBase  map[string][]string
	counts  map[overlays.AssetType]int
	widgets []overlays.Widget
	report  Report
	name    string
	width   int
	height  int
	seller  bool
}

func Import(r io.ReaderAt, size int64) (Plan, error) {
	entries, err := readArchive(r, size)
	if err != nil {
		return Plan{}, err
	}
	b := newBuilder(entries)
	if root, ok := b.shallowest(manifestName); ok {
		return b.sellerPack(root)
	}
	return b.generic()
}

func newBuilder(entries []entry) *builder {
	index := make(map[string]entry, len(entries))
	for _, e := range entries {
		index[strings.ToLower(e.Path)] = e
	}
	return &builder{
		entries: entries, index: index, used: make(map[string]bool),
		byPath: make(map[string]string), byBase: make(map[string][]string),
		counts: make(map[overlays.AssetType]int),
		name:   defaultName, width: defaultWidth, height: defaultHeight,
	}
}

func (b *builder) shallowest(base string) (entry, bool) {
	var best entry
	found := false
	for _, e := range b.entries {
		if strings.ToLower(path.Base(e.Path)) != base {
			continue
		}
		if !found || depth(e.Path) < depth(best.Path) {
			best, found = e, true
		}
	}
	return best, found
}

func depth(name string) int {
	return strings.Count(name, "/")
}

func (b *builder) take(dir, name string) (entry, bool) {
	e, ok := b.index[strings.ToLower(path.Join(dir, name))]
	if ok {
		b.used[e.Path] = true
	}
	return e, ok
}

func (b *builder) registerEntry(e entry) string {
	rule := ruleFor(e.Path)
	if reason := b.refusal(e, rule); reason != "" {
		b.report.unsupported(Subject(e.Path), reason)
		return ""
	}
	b.report.mapped(Subject(e.Path))
	return b.add(File{
		Path: e.Path, Group: groupOf(e.Path), Type: rule.kind,
		SHA256: e.SHA256, Size: e.Size, Open: e.open,
	})
}

func (b *builder) refusal(e entry, rule fileRule) Reason {
	if rule.reason != "" {
		return rule.reason
	}
	if rule.kind == overlays.AssetWidget && !b.seller {
		return ReasonUnsupportedType
	}
	return b.sizeReason(rule.kind, e.Size)
}

func (b *builder) sizeReason(kind overlays.AssetType, size int64) Reason {
	limit, _ := overlays.AssetLimitFor(kind)
	switch {
	case size == 0:
		return ReasonEmpty
	case size > limit.MaxBytes:
		return ReasonTooLarge
	case len(b.files) >= overlays.OverlayMaxAssets:
		return ReasonTooManyAssets
	case limit.MaxPerOverlay > 0 && b.counts[kind] >= limit.MaxPerOverlay:
		return ReasonTooManyOfType
	}
	return ""
}

func (b *builder) add(f File) string {
	f.ID = "f" + strconv.Itoa(len(b.files)+1)
	b.files = append(b.files, f)
	b.counts[f.Type]++
	b.byPath[strings.ToLower(f.Path)] = f.ID
	base := strings.ToLower(path.Base(f.Path))
	b.byBase[base] = append(b.byBase[base], f.ID)
	return f.ID
}

func groupOf(name string) string {
	dir := path.Dir(name)
	if dir == "." {
		return ""
	}
	return dir
}

func (b *builder) lookup(ref string) (string, bool) {
	key := strings.ToLower(path.Clean(strings.TrimPrefix(ref, "./")))
	if id, ok := b.byPath[key]; ok {
		return id, true
	}
	ids := b.byBase[path.Base(key)]
	if len(ids) != 1 {
		return "", false
	}
	return ids[0], true
}

func (b *builder) plan() Plan {
	assets := make(map[string]overlays.Asset, len(b.files))
	for _, f := range b.files {
		assets[f.ID] = overlays.Asset{Type: f.Type, Bytes: f.Size, SHA256: f.SHA256}
	}
	doc := overlays.Document{
		Version: overlays.Version, Kind: overlays.KindCustom, ID: "imported", Name: b.name,
		Width: b.width, Height: b.height, Widgets: b.widgets, Assets: assets,
	}
	return Plan{Document: doc, Files: b.files, Report: b.report}
}
