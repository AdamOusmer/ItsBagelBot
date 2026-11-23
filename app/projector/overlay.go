// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/overlays"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
)

var errOverlayChange = errors.New("overlay change without identity, version, link or kind")

func (p *Projector) HandleOverlayChanged(msg *bus.Message) error {
	return foldEvent(p, msg, eventFold[data.OverlayChangedDTO]{
		subject:  data.SubjectOverlayChanged,
		validate: validateOverlayChanged,
		apply:    p.applyOverlayChanged,
	})
}

func validateOverlayChanged(change data.OverlayChangedDTO) error {
	o := change.Overlay
	if err := validate.UserID(o.UserID); err != nil {
		return err
	}
	if o.ID == 0 || o.Revision == 0 {
		return errOverlayChange
	}
	if !o.TokenHash.Valid() || !validOverlayKind(overlays.Kind(o.Kind)) {
		return errOverlayChange
	}
	return nil
}

func validOverlayKind(kind overlays.Kind) bool {
	return slices.Contains([]overlays.Kind{overlays.KindSystem, overlays.KindCustom}, kind)
}

func (p *Projector) applyOverlayChanged(ctx context.Context, change data.OverlayChangedDTO) error {
	applied, err := p.store.FoldOverlay(ctx, change)
	if err != nil {
		return err
	}
	if applied {
		if err := p.publishOverlayChange(ctx, change); err != nil {
			return err
		}
	}
	keys := []string{string(change.Overlay.TokenHash)}
	if change.PreviousTokenHash != "" {
		keys = append(keys, string(change.PreviousTokenHash))
	}
	p.invalidations.Publish(invalidate.ScopeOverlays, change.Overlay.UserID, keys...)
	return nil
}

func (p *Projector) publishOverlayChange(ctx context.Context, change data.OverlayChangedDTO) error {
	id := change.Overlay.ID
	if change.Deleted || change.PreviousTokenHash != "" {
		return bus.PublishCore(ctx, p.nc, overlaysrpc.OverlayRevokedSubject(id), nil)
	}
	return bus.PublishCore(ctx, p.nc, overlaysrpc.OverlayRevisedSubject(id), []byte(strconv.FormatUint(change.Overlay.Revision, 10)))
}
