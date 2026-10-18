// Copyright (c) 2026 Adam Ousmer. All rights reserved.

package rpc

import (
	"context"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/giveaway"
	"ItsBagelBot/app/db/transactions/ent/giveawaycandidate"
	giveawayengine "ItsBagelBot/app/db/transactions/giveaway"
	"ItsBagelBot/internal/domain/rpc"
	giveaways "ItsBagelBot/internal/domain/rpc/giveaways"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
)

func (g *GiveawayRPC) create(ctx context.Context, req giveaways.CreateRequest) giveaways.CreateReply {
	if denied, ok := adminNewAwardGuard(g, ctx, req.ActorID, func(r rpc.Refusal) giveaways.CreateReply {
		return giveaways.CreateReply{Refusal: r, Capabilities: g.capabilities()}
	}); ok {
		return denied
	}
	row, err := g.store.Create(ctx, req, g.rulesVersion, time.Now().UTC())
	if err != nil {
		return giveaways.CreateReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	return giveaways.CreateReply{Campaign: campaignView(row), Capabilities: g.capabilities()}
}

func (g *GiveawayRPC) preview(ctx context.Context, req giveaways.PreviewRequest) giveaways.PreviewReply {
	if denied, ok := adminGuard(g, ctx, req.ActorID, g.previewRefusal); ok {
		return denied
	}
	if req.CampaignID == "" {
		if !validPreviewValues(req.WinnerCount, req.PrizeMonths) {
			return g.previewRefusal(rpc.Refused(rpc.CodeInvalid, "winner_count must be positive and prize_months must be between 1 and 12"))
		}
		return g.previewPool(ctx, nil, prizePlan{winners: req.WinnerCount, months: req.PrizeMonths})
	}
	row, err := g.db.Giveaway.Query().Where(giveaway.IDEQ(req.CampaignID)).Only(ctx)
	if err != nil {
		return g.previewRefusal(refusal(err))
	}
	return g.previewPool(ctx, row, campaignPlan(row))
}

func (g *GiveawayRPC) previewRefusal(r rpc.Refusal) giveaways.PreviewReply {
	return giveaways.PreviewReply{Refusal: r, Capabilities: g.capabilities()}
}

type prizePlan struct{ winners, months int }

func campaignPlan(c *ent.Giveaway) prizePlan {
	return prizePlan{winners: c.WinnerCount, months: c.PrizeMonths}
}

func validPreviewValues(winners, months int) bool {
	return winners > 0 && months >= 1 && months <= 12
}

func (g *GiveawayRPC) previewPool(ctx context.Context, campaign *ent.Giveaway, plan prizePlan) giveaways.PreviewReply {
	pool, err := g.users.Pool(ctx, nil)
	if err != nil {
		return g.previewRefusal(rpc.Refused(rpc.CodeUnavailable, err.Error()))
	}
	return g.previewFromPool(ctx, campaign, pool, plan)
}

func (g *GiveawayRPC) previewFromPool(ctx context.Context, campaign *ent.Giveaway, pool usersrpc.GiveawayPoolReply, plan prizePlan) giveaways.PreviewReply {
	candidates, err := g.campaignCandidates(ctx, campaign, pool)
	if err != nil {
		return g.previewRefusal(refusal(err))
	}
	digest, err := giveawayengine.PoolDigest(giveawayengine.EligibleCandidates(candidates))
	if err != nil {
		return g.previewRefusal(refusal(err))
	}
	eligible, excluded := wirePool(candidates)
	reply := giveaways.PreviewReply{Candidates: eligible, Excluded: excluded, Summary: summaryValues(pool, candidates, plan), PoolDigest: digest, Capabilities: g.capabilities()}
	if campaign != nil {
		reply.Campaign = campaignView(campaign)
	}
	return reply
}

func (g *GiveawayRPC) campaignCandidates(ctx context.Context, campaign *ent.Giveaway, pool usersrpc.GiveawayPoolReply) ([]giveawayengine.Candidate, error) {
	if campaign == nil {
		return poolCandidates(pool, nil), nil
	}
	inclusions, err := g.inclusions(ctx, campaign.ID)
	if err != nil {
		return nil, err
	}
	return poolCandidates(pool, inclusions), nil
}

func (g *GiveawayRPC) freeze(ctx context.Context, req giveaways.FreezeRequest) giveaways.FreezeReply {
	if denied, ok := adminGuard(g, ctx, req.ActorID, func(r rpc.Refusal) giveaways.FreezeReply {
		return giveaways.FreezeReply{Refusal: r, Capabilities: g.capabilities()}
	}); ok {
		return denied
	}
	if !validFreezeInput(req) {
		return giveaways.FreezeReply{Refusal: rpc.Refused(rpc.CodeInvalid, "idempotency_key and expected_version are required"), Capabilities: g.capabilities()}
	}
	if frozen, replay, err := g.store.FreezeReplay(ctx, req); err != nil {
		return giveaways.FreezeReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	} else if replay {
		return g.frozenReplay(ctx, frozen)
	}
	return g.freezePool(ctx, req)
}

func validFreezeInput(req giveaways.FreezeRequest) bool {
	return req.CampaignID != "" && req.IdempotencyKey != "" && req.ExpectedVersion != 0
}

func (g *GiveawayRPC) freezePool(ctx context.Context, req giveaways.FreezeRequest) giveaways.FreezeReply {
	pool, err := g.users.Pool(ctx, nil)
	if err != nil {
		return giveaways.FreezeReply{Refusal: rpc.Refused(rpc.CodeUnavailable, err.Error()), Capabilities: g.capabilities()}
	}
	inclusions, err := g.inclusions(ctx, req.CampaignID)
	if err != nil {
		return giveaways.FreezeReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	candidates := poolCandidates(pool, inclusions)
	frozen, err := g.store.FreezeCandidates(ctx, req, candidates, time.Now().UTC())
	if err != nil {
		return giveaways.FreezeReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	return giveaways.FreezeReply{Campaign: campaignView(frozen), Summary: summaryValues(pool, candidates, campaignPlan(frozen)), Capabilities: g.capabilities()}
}

func (g *GiveawayRPC) frozenReplay(ctx context.Context, campaign *ent.Giveaway) giveaways.FreezeReply {
	rows, err := g.db.GiveawayCandidate.Query().Where(giveawaycandidate.GiveawayIDEQ(campaign.ID)).All(ctx)
	if err != nil {
		return giveaways.FreezeReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	return giveaways.FreezeReply{Campaign: campaignView(campaign), Summary: storedSummary(campaign, rows), Capabilities: g.capabilities()}
}
