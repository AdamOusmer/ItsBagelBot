// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

func NewEligibility(loyalty LoyaltyStore, follows FollowageLookup, accounts AccountAgeLookup, winners GiveawayWinnersStore) *eligibility.Checker {
	return eligibility.NewChecker(eligibility.Ports{
		Standings: standingsPort{loyalty}, Follows: followsPort{follows}, Accounts: accountsPort{accounts}, Winners: winners,
	})
}

type standingsPort struct{ LoyaltyStore }

type followsPort struct{ FollowageLookup }

type accountsPort struct{ AccountAgeLookup }

func (p standingsPort) Snapshot(ctx context.Context, channel, viewer uint64) (loyaltyrpc.BalanceSnapshot, error) {
	return p.BalanceSnapshot(ctx, BalanceSnapshotRequest{BroadcasterID: channel, ViewerID: viewer})
}

func (p followsPort) FollowedAt(ctx context.Context, channel, viewer uint64) (time.Time, bool, error) {
	result, err := p.Lookup(ctx, FollowageQuery{BroadcasterID: fmtID(channel), TargetID: fmtID(viewer)})
	if errors.Is(err, errFollowageScope) {
		return time.Time{}, false, eligibility.ErrFollowScope
	}
	return result.FollowedAt, result.UserFound && result.Following, err
}

func (p accountsPort) CreatedAt(ctx context.Context, viewer uint64) (time.Time, bool, error) {
	result, err := p.Lookup(ctx, fmtID(viewer), "")
	return result.CreatedAt, result.UserFound, err
}

func GiveawayOpenGate(ctx context.Context, d Deps, channel uint64, rules eligibility.Rules) (bool, eligibility.Reason) {
	if !rules.NeedsStandings() {
		return true, ""
	}
	_, loyaltyOn := ReadLoyaltyConfig(ctx, d.Proj, channel)
	if !loyaltyOn {
		return eligibility.OpenGate(rules, false, "")
	}
	return eligibility.OpenGate(rules, true, string(d.LoyaltyTick.AccrualHealth(ctx, channel)))
}

func GiveawayRequirements(locale string, rules eligibility.Rules) string {
	parts := giveawayRequirementLines(locale, rules)
	if len(parts) == 0 {
		return ""
	}
	last := len(parts) - 1
	if last == 0 {
		return parts[0]
	}
	return module.KV("first", strings.Join(parts[:last], ", "), "last", parts[last]).
		WithNamespace("eligibility").ExpandString(i18n.T(locale, "eligibility.req.list"))
}

func giveawayRequirementLines(locale string, rules eligibility.Rules) []string {
	reqs := eligibility.Describe(rules)
	parts := make([]string, 0, len(reqs))
	for _, req := range reqs {
		parts = append(parts, renderRequirement(locale, req))
	}
	return parts
}

func renderRequirement(locale string, req eligibility.Requirement) string {
	value := strconv.FormatInt(req.Value, 10)
	if req.Key == "req.watch_hours" {
		value = i18n.CompactDuration(locale, time.Duration(req.Value)*time.Second)
	}
	return module.KV("value", value).WithNamespace("eligibility").
		ExpandString(i18n.T(locale, "eligibility."+req.Key))
}
