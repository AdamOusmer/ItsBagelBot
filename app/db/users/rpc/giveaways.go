// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/invalidate"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

func SubscribeGiveaways(w Wiring, prefix string, invalidationPrefixes ...string) error {
	rpc := giveawayRPC{w: w, invalidationPrefix: firstPrefix(invalidationPrefixes)}
	bind := func(suffix string, handler any) error { return bindGiveawayVerb(w, prefix+"."+suffix, handler) }
	if err := bind("pool", rpc.pool); err != nil {
		return err
	}
	if err := bind("coverage", rpc.coverage); err != nil {
		return err
	}
	if err := bind("prepare", rpc.prepare); err != nil {
		return err
	}
	if err := bind("commit", rpc.commit); err != nil {
		return err
	}
	return bind("cancel", rpc.cancel)
}

type giveawayRPC struct {
	w                  Wiring
	invalidationPrefix string
}

func firstPrefix(prefixes []string) string {
	if len(prefixes) == 0 {
		return ""
	}
	return prefixes[0]
}

func bindGiveawayVerb(w Wiring, subject string, handler any) error {
	switch h := handler.(type) {
	case func(context.Context, usersrpc.GiveawayPoolRequest) usersrpc.GiveawayPoolReply:
		return bus.Serve(w.Within(giveawayBudget), subject, h)
	case func(context.Context, usersrpc.GiveawayCoverageRequest) usersrpc.GiveawayCoverageReply:
		return bus.Serve(w.Within(giveawayBudget), subject, h)
	case func(context.Context, usersrpc.PreparePremiumGrantRequest) usersrpc.PreparePremiumGrantReply:
		return bus.Serve(w.Within(giveawayBudget), subject, h)
	case func(context.Context, usersrpc.CommitPremiumGrantRequest) usersrpc.CommitPremiumGrantReply:
		return bus.Serve(w.Within(giveawayBudget), subject, h)
	case func(context.Context, usersrpc.CommitPremiumGrantRequest) usersrpc.GiveawayCancelReply:
		return bus.Serve(w.Within(giveawayBudget), subject, h)
	default:
		return fmt.Errorf("unsupported giveaway handler for %s", subject)
	}
}

func (g giveawayRPC) pool(ctx context.Context, req usersrpc.GiveawayPoolRequest) usersrpc.GiveawayPoolReply {
	snapshot, err := g.w.Repo.GiveawayPoolSnapshot(ctx, req.CreatedBefore)
	if err != nil {
		return usersrpc.GiveawayPoolReply{Error: err.Error()}
	}
	return usersrpc.GiveawayPoolReply{Candidates: snapshot.Candidates, SnapshotAt: snapshot.SnapshotAt,
		RuleVersion: snapshot.RuleVersion, Counts: giveawayCounts(snapshot.Counts)}
}

func giveawayCounts(c repository.GiveawayPoolCounts) usersrpc.GiveawayPoolCounts {
	return usersrpc.GiveawayPoolCounts{Total: c.Total, Eligible: c.Eligible, Banned: c.Banned,
		Inactive: c.Inactive, NotOnboarded: c.NotOnboarded, TestAccount: c.TestAccount,
		VIP: c.VIP, CurrentStaff: c.CurrentStaff}
}

func (g giveawayRPC) coverage(ctx context.Context, req usersrpc.GiveawayCoverageRequest) usersrpc.GiveawayCoverageReply {
	id, err := bus.UserID(req.UserID)
	if err != nil {
		return usersrpc.GiveawayCoverageReply{Error: err.Error()}
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	view, err := g.w.Repo.PremiumCoverage(ctx, id, now)
	if err != nil {
		return usersrpc.GiveawayCoverageReply{Error: err.Error()}
	}
	return usersrpc.GiveawayCoverageReply{Coverage: &view}
}

func (g giveawayRPC) prepare(ctx context.Context, req usersrpc.PreparePremiumGrantRequest) usersrpc.PreparePremiumGrantReply {
	grant, err := g.w.Repo.PreparePremiumGrant(ctx, req)
	if err != nil {
		return usersrpc.PreparePremiumGrantReply{Error: err.Error()}
	}
	return usersrpc.PreparePremiumGrantReply{Grant: &grant}
}

func (g giveawayRPC) commit(ctx context.Context, req usersrpc.CommitPremiumGrantRequest) usersrpc.CommitPremiumGrantReply {
	grant, err := g.w.Repo.CommitPremiumGrant(ctx, req)
	if err != nil {
		return usersrpc.CommitPremiumGrantReply{Error: err.Error()}
	}
	if g.invalidationPrefix != "" {
		if err := invalidate.Publish(g.w.NC, g.invalidationPrefix, "status", fmt.Sprint(req.UserID)); err != nil {
			return usersrpc.CommitPremiumGrantReply{Error: err.Error()}
		}
	}
	return usersrpc.CommitPremiumGrantReply{Grant: &grant}
}

func (g giveawayRPC) cancel(ctx context.Context, req usersrpc.CommitPremiumGrantRequest) usersrpc.GiveawayCancelReply {
	return usersrpc.GiveawayCancelReply{Error: errorText(g.w.Repo.CancelPremiumGrant(ctx, req))}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
