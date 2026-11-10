package giveaway

import (
	"context"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/giveawayaward"
	"ItsBagelBot/app/db/transactions/ent/giveawayfulfillmentplan"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
)

type fulfillmentPlan struct {
	intervalRule string
	start, end   time.Time
}

func (e *Engine) planAward(ctx context.Context, award *ent.GiveawayAward, coverage usersrpc.PremiumCoverage) (fulfillmentPlan, error) {
	rule, err := e.intervalRuleFor(ctx, award, coverage)
	if err != nil {
		return fulfillmentPlan{}, err
	}
	if saved, found, err := e.loadStoredPlan(ctx, award, rule); err != nil {
		return fulfillmentPlan{}, err
	} else if found {
		return saved, nil
	}
	if hasLegacyProjection(award) {
		return fulfillmentPlan{}, e.needsReview(ctx, award, "legacy planned dates have no durable fulfillment plan")
	}
	return e.newFulfillmentPlan(ctx, award, coverage, rule)
}

func (e *Engine) loadStoredPlan(ctx context.Context, award *ent.GiveawayAward, rule string) (fulfillmentPlan, bool, error) {
	row, err := e.store.DB.GiveawayFulfillmentPlan.Query().Where(giveawayfulfillmentplan.AwardIDEQ(award.ID)).Only(ctx)
	if ent.IsNotFound(err) {
		return fulfillmentPlan{}, false, nil
	}
	if err != nil {
		return fulfillmentPlan{}, false, err
	}
	saved := fulfillmentPlan{intervalRule: row.IntervalRule, start: row.StartAt, end: row.EndAt}
	if saved.intervalRule != rule || projectionDiffers(award, saved) {
		return fulfillmentPlan{}, false, e.needsReview(ctx, award, "saved fulfillment plan does not match current coverage")
	}
	return saved, true, nil
}

func (e *Engine) newFulfillmentPlan(ctx context.Context, award *ent.GiveawayAward, coverage usersrpc.PremiumCoverage, rule string) (fulfillmentPlan, error) {
	start := e.coverageStart(coverage)
	end, err := PrizeInterval(start, award.PrizeMonths, rule)
	if err != nil {
		return fulfillmentPlan{}, e.needsReview(ctx, award, err.Error())
	}
	return fulfillmentPlan{intervalRule: rule, start: start, end: end}, nil
}

func (e *Engine) saveFulfillmentPlan(ctx context.Context, award *ent.GiveawayAward, plan fulfillmentPlan) (*ent.GiveawayAward, error) {
	tx, err := e.store.DB.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	row, err := e.ensurePlanRow(ctx, tx, award.ID, plan)
	if ent.IsConstraintError(err) {
		_ = tx.Rollback()
		return e.recoverFulfillmentPlan(ctx, award, plan)
	}
	if err != nil {
		return nil, err
	}
	if !plan.matches(row) {
		_ = tx.Rollback()
		return nil, e.needsReview(ctx, award, "durable fulfillment plan changed")
	}
	updated, err := e.projectFulfillmentPlan(ctx, tx, award, row)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return updated.Unwrap(), nil
}

func (e *Engine) ensurePlanRow(ctx context.Context, tx *ent.Tx, awardID string, plan fulfillmentPlan) (*ent.GiveawayFulfillmentPlan, error) {
	row, err := tx.GiveawayFulfillmentPlan.Query().Where(giveawayfulfillmentplan.AwardIDEQ(awardID)).Only(ctx)
	if !ent.IsNotFound(err) {
		return row, err
	}
	return tx.GiveawayFulfillmentPlan.Create().SetID("plan:" + awardID).SetAwardID(awardID).SetIntervalRule(plan.intervalRule).SetStartAt(plan.start).SetEndAt(plan.end).SetCreatedAt(e.now()).Save(ctx)
}

func (e *Engine) projectFulfillmentPlan(ctx context.Context, tx *ent.Tx, award *ent.GiveawayAward, row *ent.GiveawayFulfillmentPlan) (*ent.GiveawayAward, error) {
	return tx.GiveawayAward.UpdateOneID(award.ID).Where(giveawayaward.VersionEQ(award.Version)).SetPlannedStart(row.StartAt).SetPlannedEnd(row.EndAt).SetState("preparing").SetBillingState("pending").SetVersion(award.Version + 1).SetUpdatedAt(e.now()).Save(ctx)
}

func (p fulfillmentPlan) matches(row *ent.GiveawayFulfillmentPlan) bool {
	return p.intervalRule == row.IntervalRule && p.start.Equal(row.StartAt) && p.end.Equal(row.EndAt)
}

func (p fulfillmentPlan) projectionMatches(award *ent.GiveawayAward) bool {
	return optionalTimeMatches(award.PlannedStart, p.start) && optionalTimeMatches(award.PlannedEnd, p.end)
}

func optionalTimeMatches(actual, expected time.Time) bool {
	return actual.IsZero() || actual.Equal(expected)
}

func hasLegacyProjection(award *ent.GiveawayAward) bool {
	return !award.PlannedStart.IsZero() || !award.PlannedEnd.IsZero()
}

func projectionDiffers(award *ent.GiveawayAward, plan fulfillmentPlan) bool {
	return !plan.projectionMatches(award)
}

func (e *Engine) recoverFulfillmentPlan(ctx context.Context, award *ent.GiveawayAward, plan fulfillmentPlan) (*ent.GiveawayAward, error) {
	row, err := e.store.DB.GiveawayFulfillmentPlan.Query().Where(giveawayfulfillmentplan.AwardIDEQ(award.ID)).Only(ctx)
	if err != nil {
		return nil, err
	}
	if !plan.matches(row) {
		return nil, e.needsReview(ctx, award, "durable fulfillment plan changed concurrently")
	}
	return e.store.DB.GiveawayAward.Get(ctx, award.ID)
}

func (e *Engine) intervalRuleFor(ctx context.Context, award *ent.GiveawayAward, coverage usersrpc.PremiumCoverage) (string, error) {
	if recurringReference(coverage) != "" {
		if !e.config.IntervalRuleVerified {
			return "", e.needsReview(ctx, award, "provider interval rule is not verified")
		}
		return ProviderMonthlyRule, nil
	}
	if !e.config.CanSchedulePromotionalGrants() {
		return "", e.needsReview(ctx, award, "promotional grant scheduling is disabled")
	}
	return PromotionalCalendarMonthRule, nil
}

func (e *Engine) coverageStart(coverage usersrpc.PremiumCoverage) time.Time {
	start := e.now().UTC().Truncate(time.Microsecond)
	if coverage.PaidThrough != nil && coverage.PaidThrough.After(start) {
		start = coverage.PaidThrough.UTC().Truncate(time.Microsecond)
	}
	for _, grant := range coverage.Grants {
		if coverageGrantExtends(grant, start) {
			start = grant.EndAt.UTC().Truncate(time.Microsecond)
		}
	}
	return start
}

func coverageGrantExtends(grant usersrpc.PremiumGrant, start time.Time) bool {
	return grant.State == "committed" && grant.EndAt.After(start) && !grant.StartAt.After(start)
}

func recurringReference(coverage usersrpc.PremiumCoverage) string {
	if coverage.RecurringReference == nil {
		return ""
	}
	return *coverage.RecurringReference
}
