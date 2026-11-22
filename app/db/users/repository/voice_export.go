// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/db"
)

const voiceExportPageSize = 250

func (r *Users) ExportLegacyVoice(ctx context.Context, after uint64) (usersrpc.VoiceExportReply, error) {
	rows, err := db.WithQuery(ctx, r.client.User.Query().
		Where(user.IDGT(after), user.Or(
			user.TtsEnabledEQ(false), user.TtsVoiceNEQ(""), user.TtsMaxSecondsNEQ(voiceplan.MaxClipSeconds),
			user.TtsOverlayTokenHashNotNil(), user.TtsOverlayGenerationNEQ(0),
		)).
		Order(ent.Asc(user.FieldID)).Limit(voiceExportPageSize).
		Select(user.FieldID, user.FieldCreatedAt, user.FieldBanned, user.FieldTtsEnabled, user.FieldTtsVoice,
			user.FieldTtsMaxSeconds, user.FieldTtsOverlayTokenHash, user.FieldTtsOverlayGeneration).All)
	if err != nil {
		return usersrpc.VoiceExportReply{}, err
	}
	page := usersrpc.VoiceExportReply{Overlays: make([]overlaysrpc.TtsOverlay, 0, len(rows))}
	for _, row := range rows {
		page.Overlays = append(page.Overlays, legacyVoiceOverlay(row))
	}
	if len(rows) == voiceExportPageSize {
		page.NextID = rows[len(rows)-1].ID
	}
	return page, nil
}

func legacyVoiceOverlay(row *ent.User) overlaysrpc.TtsOverlay {
	out := overlaysrpc.TtsOverlay{
		UserID: row.ID, CreatedAt: row.CreatedAt.UnixMicro(), Generation: row.TtsOverlayGeneration,
		Settings: overlaysrpc.TtsSettings{Enabled: row.TtsEnabled, Voice: row.TtsVoice, MaxSeconds: row.TtsMaxSeconds},
	}
	if row.TtsOverlayTokenHash != nil && !row.Banned {
		out.TokenHash = overlaysrpc.TtsTokenHash(*row.TtsOverlayTokenHash)
	}
	return out
}
