// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/giveaway"
	"ItsBagelBot/app/db/transactions/ent/giveawayalert"
	"ItsBagelBot/app/db/transactions/ent/giveawayaward"
	"ItsBagelBot/app/db/transactions/ent/giveawaycandidate"
	"ItsBagelBot/app/db/transactions/ent/giveawaydraw"
	giveawayengine "ItsBagelBot/app/db/transactions/giveaway"
	"ItsBagelBot/internal/domain/rpc"
	giveaways "ItsBagelBot/internal/domain/rpc/giveaways"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

type GiveawayRPCConfig struct {
	Store        *giveawayengine.Store
	DB           *ent.Client
	Users        *UsersGiveawayClient
	Config       giveawayengine.Config
	RulesVersion string
	Log          *zap.Logger
}

type GiveawayRPC struct {
	store        *giveawayengine.Store
	db           *ent.Client
	users        *UsersGiveawayClient
	config       giveawayengine.Config
	rulesVersion string
	log          *zap.Logger
}

func NewGiveawayRPC(cfg GiveawayRPCConfig) *GiveawayRPC {
	return &GiveawayRPC{store: cfg.Store, db: cfg.DB, users: cfg.Users, config: cfg.Config, rulesVersion: cfg.RulesVersion, log: cfg.Log}
}

// SubscribeGiveaways installs the complete admin and user-scoped surface.
// Authorization is deliberately inside every admin handler, so adding a new
// verb cannot accidentally inherit a caller-supplied role.
func SubscribeGiveaways(w bus.RPCWiring, service *GiveawayRPC) error {
	if service == nil {
		return errors.New("giveaway rpc service is nil")
	}
	return service.bind(w)
}

func (g *GiveawayRPC) capabilities() giveaways.Capabilities {
	c := giveaways.Capabilities{NewAwardsEnabled: g.config.NewAwardsEnabled, SchedulingEnabled: g.config.CanScheduleAwards(), ProviderMutations: g.config.CanMutateProvider(), IntervalRuleVerified: g.config.IntervalRuleVerified}
	if !c.NewAwardsEnabled {
		c.Reason = "new awards are disabled by launch gate"
	} else if !c.IntervalRuleVerified {
		c.Reason = "provider interval rule is not verified"
	}
	return c
}

func (g *GiveawayRPC) newAwardsGate() rpc.Refusal {
	if g.config.CanCreateNewAwards() {
		return rpc.Refusal{}
	}
	return rpc.Refused(rpc.CodeUnavailable, "new giveaways are disabled")
}

func boundedLimit(value int) int {
	if value <= 0 || value > 100 {
		return 50
	}
	return value
}

func (g *GiveawayRPC) bind(w bus.RPCWiring) error {
	return bindGiveawaySubscriptions(w, g)
}

func bindGiveawaySubscriptions(w bus.RPCWiring, g *GiveawayRPC) error {
	prefix := giveaways.AdminPrefix + "."
	binders := []func() error{
		func() error { return bus.Serve(w, prefix+giveaways.VerbList, g.list) },
		func() error { return bus.Serve(w, prefix+giveaways.VerbGet, g.get) },
		func() error { return bus.Serve(w, prefix+giveaways.VerbCreate, g.create) },
		func() error { return bus.Serve(w, prefix+giveaways.VerbPreview, g.preview) },
		func() error { return bus.Serve(w, prefix+giveaways.VerbFreeze, g.freeze) },
		func() error { return bus.Serve(w, prefix+giveaways.VerbDraw, g.draw) },
		func() error { return bus.Serve(w, prefix+giveaways.VerbRetry, g.retry) },
		func() error { return bus.Serve(w, prefix+giveaways.VerbAlerts, g.alerts) },
		func() error { return bus.Serve(w, prefix+giveaways.VerbHistory, g.history) },
		func() error { return bus.Serve(w, prefix+giveaways.VerbCapabilities, g.capabilitiesRPC) },
		func() error { return bus.Serve(w, giveaways.MineSubject, g.mine) },
	}
	for _, bind := range binders {
		if err := bind(); err != nil {
			return err
		}
	}
	return nil
}

func refusal(err error) rpc.Refusal {
	if err == nil {
		return rpc.Refusal{}
	}
	return rpc.Refused(rpc.CodeInternal, err.Error())
}
func forbidden(err error) rpc.Refusal {
	if err == nil {
		err = errors.New("forbidden")
	}
	return rpc.Refused(rpc.CodeForbidden, err.Error())
}

func adminGuard[T any](g *GiveawayRPC, ctx context.Context, actor string, denied func(rpc.Refusal) T) (T, bool) {
	if _, refusal := g.authorize(ctx, actor); refusal.Error != "" {
		return denied(refusal), true
	}
	var zero T
	return zero, false
}

func adminNewAwardGuard[T any](g *GiveawayRPC, ctx context.Context, actor string, denied func(rpc.Refusal) T) (T, bool) {
	if result, ok := adminGuard(g, ctx, actor, denied); ok {
		return result, true
	}
	if gate := g.newAwardsGate(); gate.Error != "" {
		return denied(gate), true
	}
	var zero T
	return zero, false
}

func mapSlice[T any, R any](values []T, mapFn func(T) R) []R {
	result := make([]R, 0, len(values))
	for _, value := range values {
		result = append(result, mapFn(value))
	}
	return result
}

func (g *GiveawayRPC) authorize(ctx context.Context, actor string) (uint64, rpc.Refusal) {
	if _, err := bus.UserID(actor); err != nil {
		return 0, rpc.Refused(rpc.CodeInvalid, err.Error())
	}
	id, err := g.users.Authorize(ctx, actor)
	if err != nil {
		return 0, forbidden(err)
	}
	return id, rpc.Refusal{}
}

func (g *GiveawayRPC) draw(ctx context.Context, req giveaways.DrawRequest) giveaways.DrawReply {
	if denied, ok := adminNewAwardGuard(g, ctx, req.ActorID, func(r rpc.Refusal) giveaways.DrawReply {
		return giveaways.DrawReply{Refusal: r, Capabilities: g.capabilities()}
	}); ok {
		return denied
	}
	result, err := g.store.Draw(ctx, req, time.Now().UTC())
	if err != nil {
		return giveaways.DrawReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	return giveaways.DrawReply{Draw: drawView(result.Draw), Awards: mapSlice(result.Awards, awardView), Capabilities: g.capabilities()}
}

func (g *GiveawayRPC) list(ctx context.Context, req giveaways.ListRequest) giveaways.ListReply {
	if denied, ok := adminGuard(g, ctx, req.ActorID, func(r rpc.Refusal) giveaways.ListReply {
		return giveaways.ListReply{Refusal: r, Capabilities: g.capabilities()}
	}); ok {
		return denied
	}
	limit := boundedLimit(req.Limit)
	q := g.db.Giveaway.Query().Order(ent.Desc(giveaway.FieldCreatedAt)).Limit(limit)
	if req.Status != "" {
		q = q.Where(giveaway.StatusEQ(string(req.Status)))
	}
	rows, err := q.All(ctx)
	if err != nil {
		return giveaways.ListReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	out := giveaways.ListReply{Capabilities: g.capabilities()}
	for _, row := range rows {
		out.Campaigns = append(out.Campaigns, *campaignView(row))
	}
	return out
}

func (g *GiveawayRPC) get(ctx context.Context, req giveaways.GetRequest) giveaways.GetReply {
	if denied, ok := adminGuard(g, ctx, req.ActorID, func(r rpc.Refusal) giveaways.GetReply {
		return giveaways.GetReply{Refusal: r, Capabilities: g.capabilities()}
	}); ok {
		return denied
	}
	row, err := g.db.Giveaway.Query().Where(giveaway.IDEQ(req.CampaignID)).Only(ctx)
	if err != nil {
		return giveaways.GetReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	awards, err := g.db.GiveawayAward.Query().Where(giveawayaward.GiveawayIDEQ(row.ID)).Order(ent.Asc(giveawayaward.FieldOrdinal)).All(ctx)
	if err != nil {
		return giveaways.GetReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	candidates, err := g.db.GiveawayCandidate.Query().Where(giveawaycandidate.GiveawayIDEQ(row.ID)).Order(ent.Asc(giveawaycandidate.FieldUserID)).All(ctx)
	if err != nil {
		return giveaways.GetReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	out := giveaways.GetReply{Campaign: campaignView(row), Capabilities: g.capabilities()}
	for _, a := range awards {
		out.Awards = append(out.Awards, awardView(a))
	}
	for _, c := range candidates {
		out.Candidates = append(out.Candidates, candidateView(c))
	}
	if d, e := g.db.GiveawayDraw.Query().Where(giveawaydraw.GiveawayIDEQ(row.ID)).Only(ctx); e == nil {
		out.Draw = drawView(d)
	}
	return out
}

func (g *GiveawayRPC) retry(ctx context.Context, req giveaways.AwardRetryRequest) giveaways.AwardRetryReply {
	if denied, ok := adminGuard(g, ctx, req.ActorID, func(r rpc.Refusal) giveaways.AwardRetryReply {
		return giveaways.AwardRetryReply{Refusal: r, Capabilities: g.capabilities()}
	}); ok {
		return denied
	}
	a, err := g.db.GiveawayAward.Query().Where(giveawayaward.IDEQ(req.AwardID)).Only(ctx)
	if err != nil {
		return giveaways.AwardRetryReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	err = g.store.RetryAward(ctx, a.ID, time.Now().UTC())
	if err != nil {
		return giveaways.AwardRetryReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	view := awardView(a)
	return giveaways.AwardRetryReply{Award: &view, Capabilities: g.capabilities()}
}

func (g *GiveawayRPC) alerts(ctx context.Context, req giveaways.ListAlertsRequest) giveaways.ListAlertsReply {
	if denied, ok := adminGuard(g, ctx, req.ActorID, func(r rpc.Refusal) giveaways.ListAlertsReply {
		return giveaways.ListAlertsReply{Refusal: r, Capabilities: g.capabilities()}
	}); ok {
		return denied
	}
	limit := boundedLimit(req.Limit)
	q := g.db.GiveawayAlert.Query().Order(ent.Desc(giveawayalert.FieldLastSeenAt)).Limit(limit)
	if req.UnresolvedOnly {
		q = q.Where(giveawayalert.ResolvedAtIsNil())
	}
	rows, err := q.All(ctx)
	if err != nil {
		return giveaways.ListAlertsReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	out := giveaways.ListAlertsReply{Alerts: mapSlice(rows, alertView), Capabilities: g.capabilities()}
	return out
}

func (g *GiveawayRPC) history(ctx context.Context, req giveaways.HistoryRequest) giveaways.HistoryReply {
	if denied, ok := adminGuard(g, ctx, req.ActorID, func(r rpc.Refusal) giveaways.HistoryReply {
		return giveaways.HistoryReply{Refusal: r, Capabilities: g.capabilities()}
	}); ok {
		return denied
	}
	limit := boundedLimit(req.Limit)
	q := g.db.GiveawayAward.Query().Order(ent.Desc(giveawayaward.FieldUpdatedAt)).Limit(limit)
	if req.UserID != "" {
		id, err := bus.UserID(req.UserID)
		if err != nil {
			return giveaways.HistoryReply{Refusal: refusal(err), Capabilities: g.capabilities()}
		}
		q = q.Where(giveawayaward.UserIDEQ(id))
	}
	rows, err := q.All(ctx)
	if err != nil {
		return giveaways.HistoryReply{Refusal: refusal(err), Capabilities: g.capabilities()}
	}
	out := giveaways.HistoryReply{Awards: mapSlice(rows, awardView), Capabilities: g.capabilities()}
	return out
}

func (g *GiveawayRPC) capabilitiesRPC(ctx context.Context, req giveaways.CapabilitiesRequest) giveaways.CapabilitiesReply {
	if denied, ok := adminGuard(g, ctx, req.ActorID, func(r rpc.Refusal) giveaways.CapabilitiesReply {
		return giveaways.CapabilitiesReply{Refusal: r, Capabilities: g.capabilities()}
	}); ok {
		return denied
	}
	return giveaways.CapabilitiesReply{Capabilities: g.capabilities()}
}

func (g *GiveawayRPC) mine(ctx context.Context, req giveaways.MineRequest) giveaways.MineReply {
	id, err := bus.UserID(req.UserID)
	if err != nil {
		return giveaways.MineReply{Refusal: refusal(err)}
	}
	rows, err := g.db.GiveawayAward.Query().Where(giveawayaward.UserIDEQ(id)).Order(ent.Desc(giveawayaward.FieldUpdatedAt)).Limit(100).All(ctx)
	if err != nil {
		return giveaways.MineReply{Refusal: refusal(err)}
	}
	out := giveaways.MineReply{}
	for _, a := range rows {
		out.Awards = append(out.Awards, awardView(a))
	}
	return out
}

func poolCandidates(pool usersrpc.GiveawayPoolReply) []giveawayengine.Candidate {
	out := make([]giveawayengine.Candidate, 0, len(pool.Candidates))
	for _, c := range pool.Candidates {
		out = append(out, giveawayengine.Candidate{UserID: c.UserID, Username: c.Username, Eligible: true, Eligibility: c})
	}
	return out
}
func wireCandidates(in []giveawayengine.Candidate) []giveaways.Candidate {
	out := make([]giveaways.Candidate, 0, len(in))
	for _, c := range in {
		out = append(out, giveaways.Candidate{UserID: c.UserID, Username: c.Username, Eligible: c.Eligible, Eligibility: c.Eligibility})
	}
	return out
}
func summary(pool usersrpc.GiveawayPoolReply, c *ent.Giveaway) giveaways.PoolSummary {
	return summaryValues(pool, c.WinnerCount, c.PrizeMonths)
}
func summaryValues(pool usersrpc.GiveawayPoolReply, winners, months int) giveaways.PoolSummary {
	s := giveaways.PoolSummary{Total: pool.Counts.Total, Eligible: pool.Counts.Eligible, RequestedWinners: winners, PrizeMonths: months}
	s.Exclusions = giveaways.PoolExclusionCounts{Banned: pool.Counts.Banned, Inactive: pool.Counts.Inactive, NotOnboarded: pool.Counts.NotOnboarded, TestAccount: pool.Counts.TestAccount, VIP: pool.Counts.VIP, CurrentStaff: pool.Counts.CurrentStaff}
	s.Excluded = s.Total - s.Eligible
	for _, x := range pool.Candidates {
		if x.SubscriptionRef != nil && *x.SubscriptionRef != "" {
			s.Subscribers++
		} else if x.Status == "free" {
			s.Free++
		} else {
			s.Premium++
		}
	}
	s.TotalPrizeMonths, _ = giveawayengine.PrizeTotal(winners, months)
	return s
}

func storedSummary(campaign *ent.Giveaway, rows []*ent.GiveawayCandidate) giveaways.PoolSummary {
	s := giveaways.PoolSummary{Total: len(rows), RequestedWinners: campaign.WinnerCount, PrizeMonths: campaign.PrizeMonths}
	for _, row := range rows {
		addStoredCandidate(&s, row)
	}
	s.TotalPrizeMonths, _ = giveawayengine.PrizeTotal(campaign.WinnerCount, campaign.PrizeMonths)
	return s
}

func addStoredCandidate(summary *giveaways.PoolSummary, row *ent.GiveawayCandidate) {
	if row.Eligible {
		summary.Eligible++
		return
	}
	summary.Excluded++
	addStoredExclusion(&summary.Exclusions, row.ExclusionReason)
}

func addStoredExclusion(counts *giveaways.PoolExclusionCounts, reason string) {
	switch reason {
	case "banned":
		counts.Banned++
	case "inactive":
		counts.Inactive++
	case "not_onboarded":
		counts.NotOnboarded++
	case "test_account":
		counts.TestAccount++
	case "vip":
		counts.VIP++
	case "current_staff":
		counts.CurrentStaff++
	}
}
func campaignView(c *ent.Giveaway) *giveaways.Campaign {
	return &giveaways.Campaign{ID: c.ID, Title: c.Title, Reason: c.Reason, RulesVersion: c.RulesVersion, WinnerCount: c.WinnerCount, PrizeMonths: c.PrizeMonths, Status: giveaways.Status(c.Status), CreatedBy: c.CreatedBy, Version: c.Version, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, FrozenAt: timePtr(c.FrozenAt), DrawnAt: timePtr(c.DrawnAt)}
}
func awardView(a *ent.GiveawayAward) giveaways.Award {
	return giveaways.Award{ID: a.ID, CampaignID: a.GiveawayID, DrawID: a.DrawID, UserID: a.UserID, Ordinal: a.Ordinal, PrizeMonths: a.PrizeMonths, IntervalRule: a.IntervalRule, State: giveaways.AwardState(a.State), BillingState: giveaways.BillingState(a.BillingState), EmailState: giveaways.EmailState(a.EmailState), PlannedStart: timePtr(a.PlannedStart), PlannedEnd: timePtr(a.PlannedEnd), ConfirmedStart: timePtr(a.ConfirmedStart), ConfirmedEnd: timePtr(a.ConfirmedEnd), GrantID: a.GrantID, BillingOperationID: a.BillingOperationID, FailureReason: a.FailureReason, RetryCount: a.RetryCount, Version: a.Version, SelectedAt: a.SelectedAt, UpdatedAt: a.UpdatedAt}
}
func candidateView(c *ent.GiveawayCandidate) giveaways.Candidate {
	var v any
	_ = codec.Unmarshal([]byte(c.EligibilityJSON), &v)
	return giveaways.Candidate{ID: c.ID, UserID: c.UserID, Username: c.TwitchLogin, Eligible: c.Eligible, ExclusionReason: c.ExclusionReason, Eligibility: v}
}
func drawView(d *ent.GiveawayDraw) *giveaways.Draw {
	var ids []uint64
	_ = codec.Unmarshal([]byte(d.WinnerIdsJSON), &ids)
	actor := d.ActorID
	return &giveaways.Draw{ID: d.ID, CampaignID: d.GiveawayID, OperationKey: d.OperationKey, PoolDigest: d.PoolDigest, AlgorithmVersion: d.AlgorithmVersion, WinnerIDs: ids, ActorID: actor, CreatedAt: d.CreatedAt}
}
func alertView(a *ent.GiveawayAlert) giveaways.Alert {
	return giveaways.Alert{ID: a.ID, AwardID: a.AwardID, OperationID: a.OperationID, Category: a.Category, State: a.State, Message: a.Message, AffectedBoundary: timePtr(a.AffectedBoundary), FirstSeenAt: a.FirstSeenAt, LastSeenAt: a.LastSeenAt, AcknowledgedBy: a.AcknowledgedBy, AcknowledgedAt: timePtr(a.AcknowledgedAt), ResolvedAt: timePtr(a.ResolvedAt)}
}
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	v := t
	return &v
}
