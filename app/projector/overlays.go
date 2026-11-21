// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
)

var (
	errMissingPoweredOverlay = errors.New("powered overlay change without overlay or token")
	errPoweredOverlayOwner   = errors.New("powered overlay change for another user")
)

func (p *Projector) HandlePoweredOverlayChanged(msg *bus.Message) error {
	return foldEvent(p, msg, eventFold[data.PoweredOverlayChangedDTO]{
		subject:  data.SubjectPoweredOverlayChanged,
		validate: validatePoweredOverlayChanged,
		apply:    p.applyPoweredOverlayChanged,
	})
}

func validatePoweredOverlayChanged(dto data.PoweredOverlayChangedDTO) error {
	if err := validate.UserID(dto.UserID); err != nil {
		return err
	}
	if dto.Deleted {
		return nil
	}
	if dto.Overlay == nil || dto.Overlay.Token == "" {
		return errMissingPoweredOverlay
	}
	if dto.Overlay.UserID != dto.UserID {
		return errPoweredOverlayOwner
	}
	return nil
}

func (p *Projector) applyPoweredOverlayChanged(ctx context.Context, dto data.PoweredOverlayChangedDTO) error {
	tokens, err := p.foldPoweredOverlay(ctx, dto)
	if err != nil {
		return err
	}
	p.invalidations.Publish(invalidate.ScopeOverlays, dto.UserID, tokens...)
	return nil
}

func (p *Projector) foldPoweredOverlay(ctx context.Context, dto data.PoweredOverlayChangedDTO) ([]string, error) {
	tokens := poweredTokens(dto)
	if dto.Deleted {
		return p.store.DeletePoweredOverlay(ctx, projection.PoweredRemoval{UserID: dto.UserID, Revision: poweredRevision(dto), Tokens: tokens})
	}
	_, err := p.store.SetPoweredOverlay(ctx, projection.PoweredWrite{Overlay: *dto.Overlay, PreviousToken: dto.PreviousToken})
	return tokens, err
}

func poweredTokens(dto data.PoweredOverlayChangedDTO) []string {
	tokens := make([]string, 0, 2)
	if dto.PreviousToken != "" {
		tokens = append(tokens, dto.PreviousToken)
	}
	if dto.Overlay != nil && dto.Overlay.Token != "" {
		tokens = append(tokens, dto.Overlay.Token)
	}
	return tokens
}

func poweredRevision(dto data.PoweredOverlayChangedDTO) uint64 {
	if dto.Overlay == nil {
		return 0
	}
	return dto.Overlay.Revision
}
