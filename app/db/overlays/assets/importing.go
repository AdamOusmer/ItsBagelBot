// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package assets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"

	"ItsBagelBot/app/db/overlays/importer"
	"ItsBagelBot/app/db/overlays/media"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/overlays"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
)

const assetRefPrefix = "asset:"

type spooled struct {
	*os.File
	size int64
}

func spool(body io.Reader) (spooled, error) {
	file, err := os.CreateTemp("", "import-*.zip")
	if err != nil {
		return spooled{}, err
	}
	archive := spooled{File: file}
	archive.size, err = io.Copy(file, body)
	if err != nil {
		archive.release()
	}
	return archive, err
}

func (s spooled) release() {
	_ = s.Close()
	_ = os.Remove(s.Name())
}

func (s *Service) uploadImport(ctx context.Context, up upload) (any, error) {
	archive, err := spool(up.Body)
	if err != nil {
		return nil, err
	}
	defer archive.release()
	plan, err := importer.Import(archive.File, archive.size)
	if err != nil {
		return nil, err
	}
	if up.DryRun {
		return overlaysrpc.ImportResult{Report: reportOf(plan.Report)}, nil
	}
	return s.commitImport(ctx, up.Ticket.UserID, plan)
}

func (s *Service) commitImport(ctx context.Context, userID uint64, plan importer.Plan) (overlaysrpc.ImportResult, error) {
	ids, err := s.storeFiles(ctx, userID, plan.Files)
	if err != nil {
		return overlaysrpc.ImportResult{}, err
	}
	result := overlaysrpc.ImportResult{Report: reportOf(plan.Report)}
	for _, doc := range slices.Concat([]overlays.Document{plan.Document}, plan.Others) {
		doc, err = importedDocument(doc, ids, plan.Pack)
		if err != nil {
			return result, err
		}
		created, token, err := s.Repo.CreateCustom(ctx, userID, doc)
		if err != nil {
			return result, err
		}
		if result.OverlayID == 0 {
			result.OverlayID, result.URL = created.ID, overlaysrpc.OverlayURLPrefix+token
		}
		result.OverlayIDs = append(result.OverlayIDs, created.ID)
	}
	return result, nil
}

func (s *Service) storeFiles(ctx context.Context, userID uint64, files []importer.File) (map[string]string, error) {
	ids := make(map[string]string, len(files))
	for _, file := range files {
		asset, err := s.storeFile(ctx, userID, file)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file.Path, err)
		}
		ids[file.ID] = string(asset.ID)
	}
	return ids, nil
}

func (s *Service) storeFile(ctx context.Context, userID uint64, file importer.File) (repository.Asset, error) {
	body, err := file.Open()
	if err != nil {
		return repository.Asset{}, err
	}
	defer func() { _ = body.Close() }()
	result, data, err := media.Normalize(ctx, body, file.Size)
	if err != nil {
		return repository.Asset{}, err
	}
	return s.Store(ctx, userID, Blob{Name: cleanName(path.Base(file.Path), maxDisplayName), Result: result, Data: data})
}

func importedDocument(doc overlays.Document, ids map[string]string, pack *importer.Meta) (overlays.Document, error) {
	widgets, err := remapWidgets(doc.Widgets, ids)
	if err != nil {
		return doc, err
	}
	doc.Widgets = widgets
	renamed := make(map[string]overlays.Asset, len(doc.Assets))
	for old := range doc.Assets {
		renamed[ids[old]] = overlays.Asset{}
	}
	doc.Assets = renamed
	doc.Name = cleanName(nameOf(doc, pack), maxOverlayName)
	return doc, nil
}

func nameOf(doc overlays.Document, pack *importer.Meta) string {
	if doc.Name != "" || pack == nil {
		return doc.Name
	}
	return pack.Name
}

func remapWidgets(widgets []overlays.Widget, ids map[string]string) ([]overlays.Widget, error) {
	raw, err := json.Marshal(widgets)
	if err != nil {
		return nil, err
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	raw, err = json.Marshal(remapRefs(tree, ids))
	if err != nil {
		return nil, err
	}
	var out []overlays.Widget
	err = json.Unmarshal(raw, &out)
	return out, err
}

func remapRefs(value any, ids map[string]string) any {
	switch node := value.(type) {
	case string:
		return remapRef(node, ids)
	case map[string]any:
		for key, item := range node {
			node[key] = remapRefs(item, ids)
		}
	case []any:
		for i, item := range node {
			node[i] = remapRefs(item, ids)
		}
	}
	return value
}

func remapRef(text string, ids map[string]string) string {
	old, isRef := strings.CutPrefix(text, assetRefPrefix)
	if id, known := ids[old]; isRef && known {
		return assetRefPrefix + id
	}
	return text
}

func reportOf(report importer.Report) overlaysrpc.ImportReport {
	return overlaysrpc.ImportReport{
		Mapped: itemsOf(report.Mapped), Approximated: itemsOf(report.Approximated), Unsupported: itemsOf(report.Unsupported),
	}
}

func itemsOf(items []importer.Item) []overlaysrpc.ImportItem {
	out := make([]overlaysrpc.ImportItem, len(items))
	for i, item := range items {
		out[i] = overlaysrpc.ImportItem{Subject: string(item.Subject), Reason: string(item.Reason)}
	}
	return out
}
