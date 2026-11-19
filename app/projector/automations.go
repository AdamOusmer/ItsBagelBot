// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/internal/domain/invalidate"
	"context"
	"errors"
	"time"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/data"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

type automationOwner interface {
	Automations(ctx context.Context, userID uint64) ([]automation.Automation, error)
}

type ownerRPC struct {
	nc      *nats.Conn
	timeout time.Duration
}

func newAutomationOwner(nc *nats.Conn) automationOwner {
	return ownerRPC{nc: nc, timeout: 3 * time.Second}
}

func (o ownerRPC) Automations(ctx context.Context, userID uint64) ([]automation.Automation, error) {
	return projection.FetchAutomations(ctx, o.nc, projection.AutomationRequest{Subject: automationrpc.ProjectionAutomationsSubj, UserID: userID, Timeout: o.timeout})
}

var errMissingAutomationID = errors.New("automation change without id")

func (p *Projector) HandleAutomationChanged(msg *bus.Message) error {
	return foldEvent(p, msg, eventFold[data.AutomationChangedDTO]{
		subject:  data.SubjectAutomationChanged,
		validate: validateAutomationChanged,
		apply:    p.applyAutomationChanged,
	})
}

func validateAutomationChanged(dto data.AutomationChangedDTO) error {
	if err := validate.UserID(dto.UserID); err != nil {
		return err
	}
	if dto.ID == "" {
		return errMissingAutomationID
	}
	return nil
}

func (p *Projector) applyAutomationChanged(ctx context.Context, dto data.AutomationChangedDTO) error {
	projected, err := p.store.AutomationsProjected(ctx, dto.UserID)
	if err != nil {
		return err
	}
	if projected {
		err = p.store.ApplyAutomation(ctx, dto)
	} else {
		err = p.hydrateAutomations(ctx, dto.UserID)
	}
	if err != nil {
		return err
	}
	p.invalidations.Publish(invalidate.ScopeAutomations, dto.UserID)
	return nil
}

func (p *Projector) hydrateAutomations(ctx context.Context, userID uint64) error {
	list, err := p.owner.Automations(ctx, userID)
	if err != nil {
		return err
	}
	return p.store.SetAutomations(ctx, userID, list)
}
