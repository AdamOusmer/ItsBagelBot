// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
)

func (p *Projector) HandleFetchChanged(msg *bus.Message) error {
	return foldEvent(p, msg, eventFold[data.FetchChangedDTO]{
		subject:  data.SubjectFetchChanged,
		validate: validateFetchChanged,
		apply:    p.applyFetchChanged,
	})
}

func validateFetchChanged(dto data.FetchChangedDTO) error {
	if err := validate.UserID(dto.UserID); err != nil {
		return err
	}
	return validate.FetchDefName(dto.Name)
}

func (p *Projector) applyFetchChanged(ctx context.Context, dto data.FetchChangedDTO) error {
	if err := p.store.SetFetch(ctx, dto); err != nil {
		return err
	}
	return p.invalidations.Publish(invalidate.ScopeFetches, dto.UserID, dto.Name)
}
