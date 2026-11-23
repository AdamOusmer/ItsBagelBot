// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"strconv"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
)

func (p *Projector) handleTtsChanged(msg *bus.Message) error {
	return foldEvent(p, msg, eventFold[data.TtsOverlayChangedDTO]{
		subject:  data.SubjectTtsOverlayChanged,
		validate: validateTtsChanged,
		apply:    p.applyTtsChanged,
	})
}

func validateTtsChanged(change data.TtsOverlayChangedDTO) error {
	o := change.Overlay
	if err := validate.UserID(o.UserID); err != nil {
		return err
	}
	if o.Revision == 0 || o.CreatedAt <= 0 {
		return errors.New("missing tts version")
	}
	if o.Configured() && !o.TokenHash.Valid() {
		return errors.New("invalid tts token hash")
	}
	return o.Settings.Validate()
}

func (p *Projector) applyTtsChanged(ctx context.Context, change data.TtsOverlayChangedDTO) error {
	if _, err := p.store.FoldTtsOverlay(ctx, change); err != nil {
		return err
	}
	var keys []string
	if change.Overlay.Configured() {
		keys = append(keys, string(change.Overlay.TokenHash))
	}
	p.invalidations.Publish(invalidate.ScopeOverlays, change.Overlay.UserID, keys...)
	if !change.Revoked {
		return nil
	}
	return bus.PublishCore(ctx, p.nc, overlaysrpc.TtsRevokedPrefix+strconv.FormatUint(change.Overlay.UserID, 10), nil)
}
