// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"strconv"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/adminauth"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

var ErrCampaignSenderUnavailable = errors.New("campaign email delivery is not configured")

type CampaignTester interface {
	SendTest(ctx context.Context, campaignID int, userID uint64) error
}

type CampaignConfig struct {
	AuthPrefix string
	Prefix     string
}

type campaignRPC struct {
	repo   *repository.Users
	gate   *adminauth.Client
	tester CampaignTester
	log    *zap.Logger
}

type campaignVerb struct {
	name   string
	min    adminauth.Role
	handle func(context.Context, usersrpc.CampaignRequest) (usersrpc.CampaignReply, error)
}

func campaignRules() []domainrpc.Rule {
	return append(marketingRules(),
		domainrpc.Is(repository.ErrCampaignInvalid, domainrpc.CodeInvalid),
		domainrpc.Is(repository.ErrCampaignState, domainrpc.CodeConflict),
		domainrpc.Is(repository.ErrNotEligible, domainrpc.CodeConflict),
		domainrpc.Is(ErrCampaignSenderUnavailable, domainrpc.CodeUnavailable),
		entNotFound,
	)
}

func (c *campaignRPC) verbs() []campaignVerb {
	const (
		admin = adminauth.Admin
		owner = adminauth.Owner
	)
	return []campaignVerb{
		{"campaign_list", admin, c.list},
		{"campaign_get", admin, c.get},
		{"campaign_save", admin, c.save},
		{"campaign_preview", admin, c.preview},
		{"campaign_test", admin, c.test},
		{"campaign_approve", owner, c.approve},
		{"campaign_pause", admin, c.pause},
		{"campaign_resume", admin, c.resume},
	}
}

func (c *campaignRPC) guarded(v campaignVerb) func(context.Context, usersrpc.CampaignRequest) usersrpc.CampaignReply {
	return func(ctx context.Context, req usersrpc.CampaignRequest) usersrpc.CampaignReply {
		if _, err := c.gate.Authorize(ctx, req.ActorID, v.min); err != nil {
			return usersrpc.CampaignReply{Refusal: domainrpc.Refused(domainrpc.CodeForbidden, err.Error())}
		}
		reply, err := v.handle(ctx, req)
		if err != nil {
			return usersrpc.CampaignReply{Refusal: domainrpc.Fail(err, campaignRules()...)}
		}
		reply.OK = true
		return reply
	}
}

func SubscribeCampaigns(w Wiring, cfg CampaignConfig) error {
	c := &campaignRPC{
		repo:   w.Repo,
		gate:   adminauth.New(w.NC, adminauth.Config{AuthPrefix: cfg.AuthPrefix}),
		tester: w.Campaigns,
		log:    w.Log,
	}
	budgeted := w.Within(campaignBudget)
	for _, v := range c.verbs() {
		if err := bus.Serve(budgeted, cfg.Prefix+"."+v.name, c.guarded(v)); err != nil {
			return err
		}
	}
	return bus.Serve(w.Within(marketingBudget), usersrpc.MarketingDeliverySubject, c.deliver)
}

func campaignView(c repository.Campaign) usersrpc.CampaignView {
	return usersrpc.CampaignView{
		ID: c.ID, Name: c.Name, Revision: c.Revision, Status: c.Status, Content: c.Content, CreatedBy: c.CreatedBy, CreatedAt: c.CreatedAt,
		ApprovedBy: c.ApprovedBy, ApprovedAt: c.ApprovedAt, Sends: c.Sends,
	}
}

func campaignReply(c repository.Campaign, err error) (usersrpc.CampaignReply, error) {
	if err != nil {
		return usersrpc.CampaignReply{}, err
	}
	view := campaignView(c)
	return usersrpc.CampaignReply{Campaign: &view}, nil
}

func (c *campaignRPC) list(ctx context.Context, _ usersrpc.CampaignRequest) (usersrpc.CampaignReply, error) {
	campaigns, err := c.repo.CampaignList(ctx)
	if err != nil {
		return usersrpc.CampaignReply{}, err
	}
	views := make([]usersrpc.CampaignView, len(campaigns))
	for i, campaign := range campaigns {
		views[i] = campaignView(campaign)
	}
	return usersrpc.CampaignReply{Campaigns: views}, nil
}

func (c *campaignRPC) get(ctx context.Context, req usersrpc.CampaignRequest) (usersrpc.CampaignReply, error) {
	return campaignReply(c.repo.CampaignGet(ctx, req.ID))
}

func (c *campaignRPC) save(ctx context.Context, req usersrpc.CampaignRequest) (usersrpc.CampaignReply, error) {
	return campaignReply(c.repo.CampaignSave(ctx, repository.CampaignDraft{
		ID: req.ID, Name: req.Name, Content: req.Content, ActorID: req.ActorID,
	}))
}

func (c *campaignRPC) preview(ctx context.Context, req usersrpc.CampaignRequest) (usersrpc.CampaignReply, error) {
	campaign, err := c.repo.CampaignGet(ctx, req.ID)
	if err != nil {
		return usersrpc.CampaignReply{}, err
	}
	counts, err := c.repo.AudienceLocaleCounts(ctx)
	if err != nil {
		return usersrpc.CampaignReply{}, err
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	view := campaignView(campaign)
	return usersrpc.CampaignReply{Campaign: &view, Eligible: counts, EligibleTotal: total}, nil
}

func (c *campaignRPC) test(ctx context.Context, req usersrpc.CampaignRequest) (usersrpc.CampaignReply, error) {
	if c.tester == nil {
		return usersrpc.CampaignReply{}, ErrCampaignSenderUnavailable
	}
	staffID, err := strconv.ParseUint(req.ActorID, 10, 64)
	if err != nil {
		return usersrpc.CampaignReply{}, repository.ErrCampaignInvalid
	}
	if err := c.tester.SendTest(ctx, req.ID, staffID); err != nil {
		return usersrpc.CampaignReply{}, err
	}
	return usersrpc.CampaignReply{}, nil
}

func (c *campaignRPC) approve(ctx context.Context, req usersrpc.CampaignRequest) (usersrpc.CampaignReply, error) {
	campaign, err := c.repo.CampaignApprove(ctx, repository.CampaignApproval{ID: req.ID, Revision: req.Revision, ActorID: req.ActorID})
	if err == nil {
		c.log.Info("marketing campaign approved", zap.Int("campaign", req.ID), zap.String("actor", req.ActorID))
	}
	return campaignReply(campaign, err)
}

func (c *campaignRPC) pause(ctx context.Context, req usersrpc.CampaignRequest) (usersrpc.CampaignReply, error) {
	return campaignReply(c.repo.CampaignPause(ctx, req.ID))
}

func (c *campaignRPC) resume(ctx context.Context, req usersrpc.CampaignRequest) (usersrpc.CampaignReply, error) {
	return campaignReply(c.repo.CampaignResume(ctx, req.ID))
}

func (c *campaignRPC) deliver(ctx context.Context, req usersrpc.MarketingDeliveryRequest) usersrpc.MarketingDeliveryReply {
	if err := c.repo.RecordDelivery(ctx, req.ResendID, req.Event); err != nil {
		return usersrpc.MarketingDeliveryReply{Refusal: domainrpc.Fail(err, marketingRules()...)}
	}
	return usersrpc.MarketingDeliveryReply{OK: true}
}
