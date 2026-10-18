// Copyright (c) 2026 Adam Ousmer. All rights reserved.

package rpc

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/giveaway"
	"ItsBagelBot/app/db/transactions/ent/giveawayinclusion"
	"ItsBagelBot/internal/domain/rpc"
	giveaways "ItsBagelBot/internal/domain/rpc/giveaways"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
)

func (g *GiveawayRPC) include(ctx context.Context, req giveaways.InclusionRequest) giveaways.PreviewReply {
	actor, denied := g.authorize(ctx, req.ActorID)
	if denied.Error != "" {
		return g.previewRefusal(denied)
	}
	campaign, denied := g.draftCampaign(ctx, req.CampaignID)
	if denied.Error != "" {
		return g.previewRefusal(denied)
	}
	pool, err := g.users.Pool(ctx, nil)
	if err != nil {
		return g.previewRefusal(rpc.Refused(rpc.CodeUnavailable, err.Error()))
	}
	target, denied := includableTarget(pool, req.UserID)
	if denied.Error != "" {
		return g.previewRefusal(denied)
	}
	if err := g.saveInclusion(ctx, campaign.ID, target, actor); err != nil {
		return g.previewRefusal(refusal(err))
	}
	return g.previewFromPool(ctx, campaign, pool, campaignPlan(campaign))
}

func (g *GiveawayRPC) exclude(ctx context.Context, req giveaways.InclusionRequest) giveaways.PreviewReply {
	if denied, ok := adminGuard(g, ctx, req.ActorID, g.previewRefusal); ok {
		return denied
	}
	campaign, denied := g.draftCampaign(ctx, req.CampaignID)
	if denied.Error != "" {
		return g.previewRefusal(denied)
	}
	if _, err := g.db.GiveawayInclusion.Delete().Where(giveawayinclusion.GiveawayIDEQ(campaign.ID), giveawayinclusion.UserIDEQ(req.UserID)).Exec(ctx); err != nil {
		return g.previewRefusal(refusal(err))
	}
	return g.previewPool(ctx, campaign, campaignPlan(campaign))
}

func (g *GiveawayRPC) draftCampaign(ctx context.Context, id string) (*ent.Giveaway, rpc.Refusal) {
	campaign, err := g.db.Giveaway.Query().Where(giveaway.IDEQ(id)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, rpc.Refused(rpc.CodeNotFound, "giveaway not found")
	}
	if err != nil {
		return nil, refusal(err)
	}
	if campaign.Status != string(giveaways.StatusDraft) {
		return nil, rpc.Refused(rpc.CodeConflict, "accounts can only be added back while the giveaway is a draft")
	}
	return campaign, rpc.Refusal{}
}

func includableTarget(pool usersrpc.GiveawayPoolReply, userID uint64) (usersrpc.GiveawayCandidate, rpc.Refusal) {
	for _, c := range pool.Excluded {
		if c.UserID != userID {
			continue
		}
		if !includable(c.ExclusionReason) {
			return c, rpc.Refused(rpc.CodeInvalid, "banned accounts cannot be added back")
		}
		return c, rpc.Refusal{}
	}
	return usersrpc.GiveawayCandidate{}, rpc.Refused(rpc.CodeInvalid, "account is not excluded from the current pool")
}

func (g *GiveawayRPC) saveInclusion(ctx context.Context, campaignID string, target usersrpc.GiveawayCandidate, actor uint64) error {
	err := g.db.GiveawayInclusion.Create().SetID(campaignID + ":" + strconv.FormatUint(target.UserID, 10)).SetGiveawayID(campaignID).SetUserID(target.UserID).SetTwitchLogin(target.Username).SetExclusionReason(target.ExclusionReason).SetCreatedBy(actor).SetCreatedAt(time.Now().UTC()).Exec(ctx)
	if ent.IsConstraintError(err) {
		return nil
	}
	return err
}

func (g *GiveawayRPC) inclusions(ctx context.Context, campaignID string) (map[uint64]*ent.GiveawayInclusion, error) {
	rows, err := g.db.GiveawayInclusion.Query().Where(giveawayinclusion.GiveawayIDEQ(campaignID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[uint64]*ent.GiveawayInclusion, len(rows))
	for _, row := range rows {
		out[row.UserID] = row
	}
	return out, nil
}
