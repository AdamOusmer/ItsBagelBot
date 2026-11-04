// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/data"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/internal/domain/validate"
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
	req := automationrpc.ProjectionRequest{UserID: strconv.FormatUint(userID, 10)}
	reply, err := bus.RequestJSONTimeout[automationrpc.AutomationsProjectionReply](ctx, o.nc, automationrpc.ProjectionAutomationsSubj, req, o.timeout)
	if err != nil {
		return nil, err
	}
	if reply.Error != "" {
		return nil, errors.New(reply.Error)
	}
	return reply.Automations, nil
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
	p.broadcastCacheInvalidate(dto.UserID, "automations")
	return nil
}

func (p *Projector) hydrateAutomations(ctx context.Context, userID uint64) error {
	list, err := p.owner.Automations(ctx, userID)
	if err != nil {
		return err
	}
	return p.store.SetAutomations(ctx, userID, list)
}
