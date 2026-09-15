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
	if denied, ok := adminGuard(g, ctx, req.ActorID, func(r rpc.Refusal) giveaways.PreviewReply {
		return giveaways.PreviewReply{Refusal: r, Capabilities: g.capabilities()}
	}); ok {
		return denied
	}
	if req.CampaignID == "" {
		if !validPreviewValues(req.WinnerCount, req.PrizeMonths) {
			return giveaways.PreviewReply{Refusal: rpc.Refused(rpc.CodeInvalid, "winner_count must be positive and prize_months must be between 1 and 12"), Capabilities: g.capabilities()}
		}
		return g.previewPool(ctx, req.WinnerCount, req.PrizeMonths)
	}
	row, err := g.db.Giveaway.Query().Where(giveaway.IDEQ(req.CampaignID)).Only(ctx)
	if err != nil {
		return giveaways.PreviewReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	pool, err := g.users.Pool(ctx, nil)
	if err != nil {
		return giveaways.PreviewReply{Refusal: rpc.Refused(rpc.CodeUnavailable, err.Error()), Capabilities: g.capabilities()}
	}
	candidates := poolCandidates(pool)
	digest, err := giveawayengine.PoolDigest(candidates)
	if err != nil {
		return giveaways.PreviewReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	return giveaways.PreviewReply{Campaign: campaignView(row), Candidates: wireCandidates(candidates), Summary: summary(pool, row), PoolDigest: digest, Capabilities: g.capabilities()}
}

func validPreviewValues(winners, months int) bool {
	return winners > 0 && months >= 1 && months <= 12
}

func (g *GiveawayRPC) previewPool(ctx context.Context, winners, months int) giveaways.PreviewReply {
	pool, err := g.users.Pool(ctx, nil)
	if err != nil {
		return giveaways.PreviewReply{Refusal: rpc.Refused(rpc.CodeUnavailable, err.Error()), Capabilities: g.capabilities()}
	}
	candidates := poolCandidates(pool)
	digest, err := giveawayengine.PoolDigest(candidates)
	if err != nil {
		return giveaways.PreviewReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	return giveaways.PreviewReply{Candidates: wireCandidates(candidates), Summary: summaryValues(pool, winners, months), PoolDigest: digest, Capabilities: g.capabilities()}
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
	candidates := poolCandidates(pool)
	frozen, err := g.store.FreezeCandidates(ctx, req, candidates, time.Now().UTC())
	if err != nil {
		return giveaways.FreezeReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	return giveaways.FreezeReply{Campaign: campaignView(frozen), Summary: summary(pool, frozen), Capabilities: g.capabilities()}
}

func (g *GiveawayRPC) frozenReplay(ctx context.Context, campaign *ent.Giveaway) giveaways.FreezeReply {
	rows, err := g.db.GiveawayCandidate.Query().Where(giveawaycandidate.GiveawayIDEQ(campaign.ID)).All(ctx)
	if err != nil {
		return giveaways.FreezeReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	return giveaways.FreezeReply{Campaign: campaignView(campaign), Summary: storedSummary(campaign, rows), Capabilities: g.capabilities()}
}
