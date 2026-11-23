// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"ItsBagelBot/app/db/overlays/ent"
	"ItsBagelBot/app/db/overlays/ent/ttsviewerblock"
	"ItsBagelBot/internal/domain/event/data"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/db"
)

func (r *Overlays) TtsBlocks(ctx context.Context, userID uint64) ([]overlaysrpc.TtsViewerBlock, error) {
	rows, err := db.WithQuery(ctx, r.client.TtsViewerBlock.Query().
		Where(ttsviewerblock.UserID(userID)).Order(ent.Desc(ttsviewerblock.FieldCreatedAt), ent.Desc(ttsviewerblock.FieldID)).All)
	if err != nil {
		return nil, err
	}
	blocks := make([]overlaysrpc.TtsViewerBlock, len(rows))
	for i, row := range rows {
		blocks[i] = overlaysrpc.TtsViewerBlock{ViewerID: row.ViewerID, CreatedAt: row.CreatedAt.UnixMicro()}
		if row.ViewerLogin != nil {
			blocks[i].ViewerLogin = *row.ViewerLogin
		}
	}
	return blocks, nil
}

func (r *Overlays) BlockTtsViewer(ctx context.Context, userID uint64, viewer overlaysrpc.TtsViewerBlock) error {
	viewerID, err := canonicalViewerID(viewer.ViewerID)
	if err != nil {
		return err
	}
	if _, err := r.GetTTS(ctx, userID); err != nil {
		return err
	}
	create := r.client.TtsViewerBlock.Create().SetUserID(userID).SetViewerID(viewerID).SetCreatedAt(time.Now().UTC())
	if viewer.ViewerLogin != "" {
		create.SetViewerLogin(viewer.ViewerLogin)
	}
	insert := create.OnConflictColumns(ttsviewerblock.FieldUserID, ttsviewerblock.FieldViewerID).Ignore()
	if err := db.WithExec(ctx, insert.Exec); err != nil {
		return err
	}
	return r.announceBlocks(ctx, userID)
}

func (r *Overlays) UnblockTtsViewer(ctx context.Context, userID uint64, viewerID string) error {
	viewerID, err := canonicalViewerID(viewerID)
	if err != nil {
		return err
	}
	if _, err := db.WithQuery(ctx, r.client.TtsViewerBlock.Delete().
		Where(ttsviewerblock.UserID(userID), ttsviewerblock.ViewerID(viewerID)).Exec); err != nil {
		return err
	}
	return r.announceBlocks(ctx, userID)
}

func (r *Overlays) announceBlocks(ctx context.Context, userID uint64) error {
	current, err := r.GetTTS(ctx, userID)
	if err != nil {
		return err
	}
	return r.announceTts(ctx, data.TtsOverlayChangedDTO{Overlay: current})
}

func (r *Overlays) blockedViewerIDs(ctx context.Context, userID uint64) ([]string, error) {
	return db.WithQuery(ctx, r.client.TtsViewerBlock.Query().
		Where(ttsviewerblock.UserID(userID)).Order(ent.Asc(ttsviewerblock.FieldViewerID)).Select(ttsviewerblock.FieldViewerID).Strings)
}

func canonicalViewerID(raw string) (string, error) {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return "", fmt.Errorf("%w: invalid viewer id", ErrInvalidConfig)
	}
	return strconv.FormatUint(id, 10), nil
}
