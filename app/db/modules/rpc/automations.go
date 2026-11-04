// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/automation"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/pkg/bus"
)

type automationsRPC struct {
	repo *repository.Automations
}

func SubscribeAutomations(w bus.RPCWiring, repo *repository.Automations) error {
	a := &automationsRPC{repo: repo}
	prefix := automationrpc.AutomationPrefix + "."
	verbs := []func() error{
		func() error {
			return bus.ServeForUser[automationrpc.ListRequest](w, prefix+automationrpc.VerbList, a.handleList)
		},
		func() error {
			return bus.ServeForUser[automationrpc.ListRequest](w, prefix+automationrpc.VerbSummaries, a.handleSummaries)
		},
		func() error {
			return bus.ServeForUser[automationrpc.GetRequest](w, prefix+automationrpc.VerbGet, a.handleGet)
		},
		func() error {
			return bus.ServeForUser[automationrpc.UpsertRequest](w, prefix+automationrpc.VerbUpsert, a.handleUpsert)
		},
		func() error {
			return bus.ServeForUser[automationrpc.UpgradeAlertRequest](w, prefix+automationrpc.VerbUpgrade, a.handleUpgradeAlert)
		},
		func() error {
			return bus.ServeForUser[automationrpc.SetEnabledRequest](w, prefix+automationrpc.VerbSetEnabled, a.handleSetEnabled)
		},
		func() error {
			return bus.ServeForUser[automationrpc.DowngradeAlertRequest](w, prefix+automationrpc.VerbDowngrade, a.handleDowngradeAlert)
		},
		func() error {
			return bus.ServeForUser[automationrpc.DeleteRequest](w, prefix+automationrpc.VerbDelete, a.handleDelete)
		},
		func() error {
			return bus.ServeForUser[automationrpc.ValidateRequest](w, prefix+automationrpc.VerbValidate, a.handleValidate)
		},
		func() error {
			return bus.ServeForUser[automationrpc.ProjectionRequest](w, automationrpc.ProjectionAutomationsSubj, a.handleProjection)
		},
	}
	for _, serve := range verbs {
		if err := serve(); err != nil {
			return err
		}
	}
	return nil
}

func automationRefusal(err error) domainrpc.Refusal {
	var refused *repository.AutomationRefusal
	if errors.As(err, &refused) {
		return domainrpc.Refused(refused.Code, refused.Error())
	}
	return bus.Classify(err,
		domainrpc.Is(repository.ErrAutomationNotFound, domainrpc.CodeNotFound),
		domainrpc.Is(repository.ErrRevisionConflict, domainrpc.CodeConflict),
	)
}

func (a *automationsRPC) handleList(ctx context.Context, _ automationrpc.ListRequest, id uint64) (automationrpc.ListReply, error) {
	items, err := a.repo.List(ctx, id)
	return automationrpc.ListReply{Automations: items, Refusal: automationRefusal(err)}, nil
}

func (a *automationsRPC) handleSummaries(ctx context.Context, _ automationrpc.ListRequest, id uint64) (automationrpc.SummariesReply, error) {
	items, err := a.repo.ListSummaries(ctx, id)
	return automationrpc.SummariesReply{Automations: items, Refusal: automationRefusal(err)}, nil
}

func (a *automationsRPC) handleSetEnabled(ctx context.Context, req automationrpc.SetEnabledRequest, id uint64) (automationrpc.UpsertReply, error) {
	return savedReply(a.repo.SetEnabled(ctx, id, repository.EnabledChange{ID: req.ID, Enabled: req.Enabled, ExpectedRevision: req.ExpectedRevision})), nil
}

func savedReply(saved automation.Automation, err error) automationrpc.UpsertReply {
	if err != nil {
		return upsertRefusal(err)
	}
	return automationrpc.UpsertReply{Automation: &saved}
}

func (a *automationsRPC) handleProjection(ctx context.Context, _ automationrpc.ProjectionRequest, id uint64) (automationrpc.AutomationsProjectionReply, error) {
	items, err := a.repo.List(ctx, id)
	return automationrpc.AutomationsProjectionReply{Automations: items, Refusal: automationRefusal(err)}, nil
}

func (a *automationsRPC) handleGet(ctx context.Context, req automationrpc.GetRequest, id uint64) (automationrpc.GetReply, error) {
	item, found, err := a.repo.Get(ctx, id, req.ID)
	if err == nil && !found {
		return automationrpc.GetReply{Refusal: domainrpc.Refused(domainrpc.CodeNotFound, "automation not found")}, nil
	}
	return automationrpc.GetReply{Automation: item, Refusal: automationRefusal(err)}, nil
}

func (a *automationsRPC) handleUpsert(ctx context.Context, req automationrpc.UpsertRequest, id uint64) (automationrpc.UpsertReply, error) {
	return savedReply(a.repo.Upsert(ctx, id, repository.AutomationInput{
		ID: req.ID, Name: req.Name, Enabled: req.Enabled,
		Definition: req.Definition, Preview: req.Preview, ExpectedRevision: req.ExpectedRevision,
	})), nil
}

func (a *automationsRPC) handleUpgradeAlert(ctx context.Context, req automationrpc.UpgradeAlertRequest, id uint64) (automationrpc.UpsertReply, error) {
	return savedReply(a.repo.UpgradeAlert(ctx, id,
		repository.AutomationInput{Name: req.Name, Enabled: req.Enabled, Definition: req.Definition, Preview: req.Preview},
		repository.AlertUpgrade{EnableKey: req.EnableKey, ExpectedModuleRev: req.ExpectedModuleRev})), nil
}

func (a *automationsRPC) handleDowngradeAlert(ctx context.Context, req automationrpc.DowngradeAlertRequest, id uint64) (automationrpc.DowngradeAlertReply, error) {
	err := a.repo.DowngradeToAlert(ctx, id, req.ID, repository.AlertDowngrade{
		EnableKey: req.EnableKey, Message: req.Message, Enabled: req.Enabled,
		ExpectedRevision: req.ExpectedRevision, ExpectedModuleRev: req.ExpectedModuleRev,
	})
	reply := automationrpc.DowngradeAlertReply{Refusal: automationRefusal(err)}
	reply.Conflict = reply.Code == domainrpc.CodeConflict
	return reply, nil
}

func upsertRefusal(err error) automationrpc.UpsertReply {
	reply := automationrpc.UpsertReply{Refusal: automationRefusal(err)}
	reply.Conflict = reply.Code == domainrpc.CodeConflict
	var refused *repository.AutomationRefusal
	if errors.As(err, &refused) {
		reply.Issues = refused.Issues
	}
	return reply
}

func (a *automationsRPC) handleDelete(ctx context.Context, req automationrpc.DeleteRequest, id uint64) (automationrpc.DeleteReply, error) {
	deleted, err := a.repo.Delete(ctx, id, req.ID)
	return automationrpc.DeleteReply{Deleted: deleted, Refusal: automationRefusal(err)}, nil
}

func (a *automationsRPC) handleValidate(ctx context.Context, req automationrpc.ValidateRequest, id uint64) (automationrpc.ValidateReply, error) {
	issues, err := a.repo.Validate(ctx, id, req.Name, req.Definition)
	return automationrpc.ValidateReply{Issues: issues, Refusal: automationRefusal(err)}, nil
}
