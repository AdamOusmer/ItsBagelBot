package giveaway

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/billingoperation"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
)

type billingPlan struct {
	award      *ent.GiveawayAward
	coverage   usersrpc.PremiumCoverage
	reference  string
	start, end time.Time
}

func (e *Engine) prepareBilling(ctx context.Context, plan billingPlan) (string, error) {
	reference := recurringReference(plan.coverage)
	if plan.coverage.BillingUncertain || plan.coverage.CancelPending {
		return "", e.needsReview(ctx, plan.award, "Users reported billing uncertainty or cancellation pending")
	}
	if reference == "" {
		return "", nil
	}
	plan.reference = reference
	if err := e.ensureBillingOperation(ctx, plan); err != nil {
		return "", err
	}
	return reference, nil
}

func (e *Engine) ensureBillingOperation(ctx context.Context, plan billingPlan) error {
	operation, err := e.store.DB.BillingOperation.Query().Where(billingoperation.AwardIDEQ(plan.award.ID)).Only(ctx)
	if ent.IsNotFound(err) {
		operation, err = e.store.DB.BillingOperation.Create().SetID("billing:" + plan.award.ID).SetAwardID(plan.award.ID).SetAgreementID(fmt.Sprintf("agreement:%d:%s", plan.award.UserID, plan.reference)).SetRecurringReference(plan.reference).SetRequestedStart(plan.start).SetRequestedEnd(plan.end).Save(ctx)
	}
	if err != nil {
		return err
	}
	if billingPlanChanged(operation, plan) {
		return e.needsReview(ctx, plan.award, "durable billing operation plan changed")
	}
	if plan.award.BillingOperationID == operation.ID {
		return nil
	}
	_, err = plan.award.Update().SetBillingOperationID(operation.ID).SetUpdatedAt(e.now()).Save(ctx)
	return err
}

func billingPlanChanged(operation *ent.BillingOperation, plan billingPlan) bool {
	return !operation.RequestedStart.Equal(plan.start) || !operation.RequestedEnd.Equal(plan.end) || operation.RecurringReference != plan.reference
}
