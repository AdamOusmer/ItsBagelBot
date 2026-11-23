// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/app/db/overlays/ent"
	"ItsBagelBot/app/db/overlays/ent/predicate"
	"ItsBagelBot/app/db/overlays/ent/ttsoverlay"
	"ItsBagelBot/app/db/overlays/ent/ttsviewerblock"
	"ItsBagelBot/internal/domain/event/data"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
)

var ErrTtsPlan = errors.New("voice plan required")

func (r *Overlays) GetTTS(ctx context.Context, userID uint64) (overlaysrpc.TtsOverlay, error) {
	current, err := r.findTts(ctx, ttsoverlay.UserID(userID))
	if !errors.Is(err, ErrNotFound) {
		return current, err
	}
	return r.createTts(ctx, overlaysrpc.TtsOverlay{UserID: userID, Settings: overlaysrpc.DefaultTtsSettings()})
}

func (r *Overlays) createTts(ctx context.Context, initial overlaysrpc.TtsOverlay) (overlaysrpc.TtsOverlay, error) {
	initial.Revision = 1
	if initial.CreatedAt == 0 {
		initial.CreatedAt = time.Now().UnixMicro()
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.TtsOverlay, error) {
		create := r.client.TtsOverlay.Create().SetUserID(initial.UserID).SetEnabled(initial.Settings.Enabled).
			SetVoice(initial.Settings.Voice).SetMaxSeconds(initial.Settings.MaxSeconds).
			SetGeneration(initial.Generation).SetRevision(initial.Revision)
		if initial.Configured() {
			create.SetTokenHash(string(initial.TokenHash))
		}
		create.SetCreatedAt(time.UnixMicro(initial.CreatedAt).UTC())
		return create.Save(ctx)
	})
	if ent.IsConstraintError(err) {
		return r.existingTts(ctx, initial.UserID, err)
	}
	if err != nil {
		return overlaysrpc.TtsOverlay{}, err
	}
	saved := ttsOf(row)
	return saved, r.announceTts(ctx, data.TtsOverlayChangedDTO{Overlay: saved})
}

func (r *Overlays) existingTts(ctx context.Context, userID uint64, insertErr error) (overlaysrpc.TtsOverlay, error) {
	row, err := r.findTts(ctx, ttsoverlay.UserID(userID))
	if errors.Is(err, ErrNotFound) {
		return row, insertErr
	}
	return row, err
}

func (r *Overlays) SaveTTS(ctx context.Context, userID uint64, req overlaysrpc.TtsSaveRequest) (overlaysrpc.TtsOverlay, error) {
	if err := req.Settings.Validate(); err != nil {
		return overlaysrpc.TtsOverlay{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	current, err := r.GetTTS(ctx, userID)
	if err != nil {
		return overlaysrpc.TtsOverlay{}, err
	}
	if current.Revision != req.Revision {
		return overlaysrpc.TtsOverlay{}, ErrRevisionConflict
	}
	saved := current
	saved.Settings = req.Settings
	return r.writeTts(ctx, saved, false)
}

func (r *Overlays) RotateTTS(ctx context.Context, userID uint64, plan voiceplan.Plan) (overlay overlaysrpc.TtsOverlay, token string, err error) {
	if voiceplan.MonthlySeconds(plan) == 0 {
		return overlay, "", ErrTtsPlan
	}
	current, err := r.GetTTS(ctx, userID)
	if err != nil {
		return overlay, "", err
	}
	token = NewToken()
	sum := sha256.Sum256([]byte(token))
	current.TokenHash = overlaysrpc.TtsTokenHash(hex.EncodeToString(sum[:]))
	current.Generation = max(current.Generation+1, int(time.Now().UnixMicro()))
	overlay, err = r.writeTts(ctx, current, true)
	return overlay, token, err
}

func (r *Overlays) writeTts(ctx context.Context, next overlaysrpc.TtsOverlay, revoked bool) (overlaysrpc.TtsOverlay, error) {
	n, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		update := r.client.TtsOverlay.Update().Where(ttsoverlay.UserID(next.UserID),
			ttsoverlay.CreatedAtEQ(time.UnixMicro(next.CreatedAt).UTC()), ttsoverlay.Revision(next.Revision)).
			SetEnabled(next.Settings.Enabled).SetVoice(next.Settings.Voice).SetMaxSeconds(next.Settings.MaxSeconds).
			SetGeneration(next.Generation).SetRevision(next.Revision + 1)
		if next.Configured() {
			update.SetTokenHash(string(next.TokenHash))
		} else {
			update.ClearTokenHash()
		}
		return update.Save(ctx)
	})
	if err != nil {
		return overlaysrpc.TtsOverlay{}, err
	}
	if n == 0 {
		return overlaysrpc.TtsOverlay{}, ErrRevisionConflict
	}
	next.Revision++
	return next, r.announceTts(ctx, data.TtsOverlayChangedDTO{Overlay: next, Revoked: revoked})
}

func (r *Overlays) TTSByHash(ctx context.Context, hash overlaysrpc.TtsTokenHash) (overlaysrpc.TtsOverlay, error) {
	if !hash.Valid() {
		return overlaysrpc.TtsOverlay{}, ErrNotFound
	}
	return r.findTts(ctx, ttsoverlay.TokenHashEQ(string(hash)))
}

func (r *Overlays) findTts(ctx context.Context, where ...predicate.TtsOverlay) (overlaysrpc.TtsOverlay, error) {
	row, err := db.WithQuery(ctx, r.client.TtsOverlay.Query().Where(where...).Only)
	if ent.IsNotFound(err) {
		return overlaysrpc.TtsOverlay{}, ErrNotFound
	}
	if err != nil {
		return overlaysrpc.TtsOverlay{}, err
	}
	view := ttsOf(row)
	view.BlockedViewers, err = r.blockedViewerIDs(ctx, row.UserID)
	return view, err
}

func ttsOf(row *ent.TtsOverlay) overlaysrpc.TtsOverlay {
	view := overlaysrpc.TtsOverlay{
		UserID: row.UserID, Settings: overlaysrpc.TtsSettings{Enabled: row.Enabled, Voice: row.Voice, MaxSeconds: row.MaxSeconds},
		Generation: row.Generation, Revision: row.Revision, CreatedAt: row.CreatedAt.UnixMicro(),
	}
	if row.TokenHash != nil {
		view.TokenHash = overlaysrpc.TtsTokenHash(*row.TokenHash)
	}
	return view
}

func (r *Overlays) RevokeBannedTTS(ctx context.Context, ban data.UserBannedDTO) error {
	bannedAt := ban.BannedAt.UTC().Truncate(time.Microsecond)
	current, err := r.findTts(ctx, ttsoverlay.UserID(ban.UserID), ttsoverlay.CreatedAtLTE(bannedAt),
		ttsoverlay.TokenHashNotNil(), ttsoverlay.GenerationLTE(int(bannedAt.UnixMicro())))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	current.TokenHash = ""
	current.Generation++
	_, err = r.writeTts(ctx, current, true)
	return err
}

func (r *Overlays) deleteTts(ctx context.Context, userID uint64, cutoff time.Time) error {
	cutoff = cutoff.UTC().Truncate(time.Microsecond)
	return db.WithExec(ctx, func(ctx context.Context) error {
		return db.WithTx(ctx, r.client, func(tx *ent.Tx) error {
			row, err := tx.TtsOverlay.Query().Where(ttsoverlay.UserID(userID), ttsoverlay.CreatedAtLTE(cutoff)).Only(ctx)
			if ent.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			n, err := tx.TtsOverlay.Delete().Where(ttsoverlay.ID(row.ID), ttsoverlay.Revision(row.Revision)).Exec(ctx)
			if err != nil {
				return err
			}
			if n == 0 {
				return ErrRevisionConflict
			}
			if _, err := tx.TtsViewerBlock.Delete().Where(ttsviewerblock.UserID(userID), ttsviewerblock.CreatedAtLTE(cutoff)).Exec(ctx); err != nil {
				return err
			}
			return r.announceTts(ctx, data.TtsOverlayChangedDTO{Overlay: ttsOf(row), Deleted: true, Revoked: true})
		})
	})
}

func (r *Overlays) announceTts(ctx context.Context, change data.TtsOverlayChangedDTO) error {
	return bus.PublishJSON(ctx, r.pub, data.SubjectTtsOverlayChanged, change)
}
