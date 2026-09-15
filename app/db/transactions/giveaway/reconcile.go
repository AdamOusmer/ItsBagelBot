package giveaway

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/billingoperation"
	"ItsBagelBot/app/db/transactions/ent/giveawayalert"
	"ItsBagelBot/app/db/transactions/ent/giveawayaward"
	"ItsBagelBot/app/db/transactions/tebex"
	"ItsBagelBot/pkg/codec"

	giveawaysrpc "ItsBagelBot/internal/domain/rpc/giveaways"
)

const (
	providerReconcileTimeout = 75 * time.Second
	providerNearBoundary     = 2 * time.Hour
)

// reconcileProviders observes protected subscriptions and records provider
// drift. It never writes Active or otherwise attempts to undo a customer
// cancellation; an award already granted remains intact for review.
func (e *Engine) reconcileProviders(ctx context.Context) error {
	if e.provider == nil {
		return nil
	}
	rows, err := e.store.DB.GiveawayAward.Query().Where(
		giveawayaward.StateIn(string(giveawaysrpc.AwardScheduled), string(giveawaysrpc.AwardActive), string(giveawaysrpc.AwardCompleted)),
		giveawayaward.BillingStateIn(string(giveawaysrpc.BillingProtected), string(giveawaysrpc.BillingUncertain), string(giveawaysrpc.BillingIncident)),
	).All(ctx)
	if err != nil {
		return err
	}
	now := e.now()
	var firstErr error
	for _, award := range rows {
		if err := e.reconcileAwardProvider(ctx, award, now); err != nil && !errors.Is(err, errReconciliationSkipped) {
			if alertErr := e.alert(ctx, award.ID, "billing-reconciliation", err.Error()); alertErr != nil {
				firstErr = errors.Join(firstErr, alertErr)
			}
		}
	}
	return firstErr
}

var errReconciliationSkipped = errors.New("provider reconciliation not due")

func (e *Engine) reconcileAwardProvider(ctx context.Context, award *ent.GiveawayAward, now time.Time) error {
	claimed, ok, err := e.claimDueProvider(ctx, award, now)
	if err != nil || !ok {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, providerReconcileTimeout)
	defer cancel()
	payment, getErr := e.providerProbe(probeCtx, award.UserID, claimed.RecurringReference)
	if getErr == nil {
		getErr = (agreementSnapshot{userID: award.UserID, operation: claimed, payment: payment, now: now}).persist(probeCtx, e.store.DB)
	}
	reason := getErr
	if reason == nil {
		reason = providerDrift(payment, claimed.RecurringReference, award.PlannedEnd, now)
	}
	return providerResult{db: e.store.DB, operation: claimed, payment: payment, reason: reason, now: now}.finish(ctx)
}

func (e *Engine) claimDueProvider(ctx context.Context, award *ent.GiveawayAward, now time.Time) (*ent.BillingOperation, bool, error) {
	operation, err := e.store.DB.BillingOperation.Query().Where(billingoperation.AwardIDEQ(award.ID)).Only(ctx)
	if err != nil {
		return nil, false, err
	}
	agreement, agreementErr := e.store.DB.TebexAgreement.Get(ctx, operation.AgreementID)
	if agreementErr != nil && !ent.IsNotFound(agreementErr) {
		return nil, false, agreementErr
	}
	if !(reconcilePlan{award: award, operation: operation, agreement: agreement, now: now, config: e.config}).due() {
		return nil, false, nil
	}
	claimed, ok, err := claimProviderOperation(ctx, operation, now)
	return claimed, ok, err
}

type reconcilePlan struct {
	award     *ent.GiveawayAward
	operation *ent.BillingOperation
	agreement *ent.TebexAgreement
	now       time.Time
	config    Config
}

func (plan reconcilePlan) due() bool {
	boundary := plan.config.BoundaryInterval
	if boundary <= 0 {
		boundary = time.Minute
	}
	if !plan.operation.UpdatedAt.IsZero() && plan.now.Before(plan.operation.UpdatedAt.Add(boundary)) {
		return false
	}
	if plan.nearAwardBoundary() {
		return true
	}
	if plan.nearAgreementBoundary() {
		return true
	}
	interval := plan.config.ReconcileInterval
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	return plan.operation.UpdatedAt.IsZero() || !plan.now.Before(plan.operation.UpdatedAt.Add(interval))
}

func (plan reconcilePlan) nearAwardBoundary() bool {
	return nearProviderBoundary(plan.award.PlannedStart, plan.now) || nearProviderBoundary(plan.award.PlannedEnd, plan.now)
}

func (plan reconcilePlan) nearAgreementBoundary() bool {
	return plan.agreement != nil && nearProviderBoundary(plan.agreement.NextCollectionAt, plan.now)
}

func (e *Engine) providerProbe(ctx context.Context, userID uint64, reference string) (tebex.RecurringPayment, error) {
	if err := e.currentReference(ctx, userID, reference); err != nil {
		return tebex.RecurringPayment{}, err
	}
	return e.provider.GetRecurring(ctx, reference)
}

func nearProviderBoundary(boundary, now time.Time) bool {
	return !boundary.IsZero() && !boundary.Before(now) && boundary.Sub(now) <= providerNearBoundary
}

func (e *Engine) currentReference(ctx context.Context, userID uint64, expected string) error {
	if e.users == nil {
		return nil
	}
	coverage, err := e.users.Coverage(ctx, userID)
	if err != nil {
		return errors.New("current subscription coverage unavailable")
	}
	if coverage.BillingUncertain || coverage.CancelPending {
		return errors.New("current subscription reference changed or became uncertain")
	}
	if recurringReference(coverage) != expected {
		return errors.New("current subscription reference changed or became uncertain")
	}
	return nil
}

func claimProviderOperation(ctx context.Context, operation *ent.BillingOperation, now time.Time) (*ent.BillingOperation, bool, error) {
	lease := now.Add(providerReconcileTimeout)
	claimed, err := operation.Update().Where(
		billingoperation.VersionEQ(operation.Version),
		billingoperation.Or(billingoperation.LeaseUntilIsNil(), billingoperation.LeaseUntilLT(now)),
	).SetLeaseUntil(lease).SetVersion(operation.Version + 1).SetUpdatedAt(now).Save(ctx)
	if ent.IsNotFound(err) {
		return nil, false, nil
	}
	return claimed, err == nil, err
}

func providerDrift(payment tebex.RecurringPayment, reference string, end, now time.Time) error {
	if !payment.MatchesReference(reference) {
		return errors.New("provider recurring reference no longer matches the award")
	}
	if paymentHasCancellation(payment) {
		return errors.New("provider reports cancellation intent for a protected award")
	}
	if paymentStateAmbiguous(payment, reference) {
		return errors.New("provider subscription state is ambiguous for a protected award")
	}
	if protectionExpired(payment, reference, end, now) {
		return errors.New("provider protection no longer covers the award interval")
	}
	return nil
}

func protectionExpired(payment tebex.RecurringPayment, reference string, end, now time.Time) bool {
	if end.IsZero() || !end.After(now) {
		return false
	}
	return !payment.ProtectedThrough(reference, end)
}

func paymentStateAmbiguous(payment tebex.RecurringPayment, reference string) bool {
	return payment.Ambiguous || !payment.CanProtect(reference)
}

type agreementSnapshot struct {
	userID    uint64
	operation *ent.BillingOperation
	payment   tebex.RecurringPayment
	now       time.Time
}

func (snapshot agreementSnapshot) persist(ctx context.Context, db *ent.Client) error {
	encoded, err := codec.Marshal(snapshot.payment)
	if err != nil {
		return err
	}
	agreement, err := db.TebexAgreement.Get(ctx, snapshot.operation.AgreementID)
	if ent.IsNotFound(err) {
		return snapshot.create(ctx, db, string(encoded))
	} else if err != nil {
		return err
	}
	return snapshot.update(ctx, agreement, string(encoded))
}

func (snapshot agreementSnapshot) create(ctx context.Context, db *ent.Client, encoded string) error {
	_, err := db.TebexAgreement.Create().SetID(snapshot.operation.AgreementID).SetUserID(snapshot.userID).SetStoreID("tebex-checkout").SetRecurringReference(snapshot.operation.RecurringReference).SetInterval(snapshot.payment.Interval).SetProviderStatus(providerStatus(snapshot.payment)).SetCancelRequested(paymentHasCancellation(snapshot.payment)).SetNillableNextCollectionAt(snapshot.payment.NextPaymentDate).SetNillablePausedUntil(snapshot.payment.PausedUntil).SetLastSnapshotJSON(encoded).SetVerifiedAt(snapshot.now).SetCreatedAt(snapshot.now).SetUpdatedAt(snapshot.now).Save(ctx)
	return err
}

func (snapshot agreementSnapshot) update(ctx context.Context, agreement *ent.TebexAgreement, encoded string) error {
	update := agreement.Update().SetProviderStatus(providerStatus(snapshot.payment)).SetCancelRequested(paymentHasCancellation(snapshot.payment)).SetLastSnapshotJSON(string(encoded)).SetVerifiedAt(snapshot.now).SetUpdatedAt(snapshot.now)
	if snapshot.payment.NextPaymentDate != nil {
		update.SetNextCollectionAt(*snapshot.payment.NextPaymentDate)
	} else {
		update.ClearNextCollectionAt()
	}
	if snapshot.payment.PausedUntil != nil {
		update.SetPausedUntil(*snapshot.payment.PausedUntil)
	} else {
		update.ClearPausedUntil()
	}
	_, err := update.Save(ctx)
	return err
}

func providerStatus(payment tebex.RecurringPayment) string {
	if payment.Status == "" {
		return "unknown"
	}
	return payment.Status
}

type providerResult struct {
	db        *ent.Client
	operation *ent.BillingOperation
	payment   tebex.RecurringPayment
	reason    error
	now       time.Time
}

func (result providerResult) finish(ctx context.Context) error {
	if result.reason != nil {
		return result.finishIssue(ctx)
	}
	return result.finishVerified(ctx)
}

func (result providerResult) finishIssue(ctx context.Context) error {
	if _, err := result.db.GiveawayAward.UpdateOneID(result.operation.AwardID).SetBillingState(string(giveawaysrpc.BillingUncertain)).SetFailureReason(result.reason.Error()).SetUpdatedAt(result.now).Save(ctx); err != nil {
		return err
	}
	if err := upsertProviderAlert(ctx, providerAlert{db: result.db, awardID: result.operation.AwardID, operationID: result.operation.ID, message: result.reason.Error(), now: result.now}); err != nil {
		return err
	}
	_, err := result.operation.Update().Where(billingoperation.VersionEQ(result.operation.Version)).ClearLeaseUntil().SetVersion(result.operation.Version + 1).SetState("needs_review").SetLastError(result.reason.Error()).SetUpdatedAt(result.now).Save(ctx)
	return err
}

func (result providerResult) finishVerified(ctx context.Context) error {
	encoded, err := codec.Marshal(result.payment)
	if err != nil {
		return err
	}
	_, err = result.operation.Update().Where(billingoperation.VersionEQ(result.operation.Version)).ClearLeaseUntil().SetVersion(result.operation.Version + 1).SetState("verified").SetAfterSnapshotJSON(string(encoded)).SetVerifiedAt(result.now).SetLastError("").SetUpdatedAt(result.now).Save(ctx)
	return err
}

type providerAlert struct {
	db                            *ent.Client
	awardID, operationID, message string
	now                           time.Time
}

func upsertProviderAlert(ctx context.Context, alertData providerAlert) error {
	alert, err := alertData.db.GiveawayAlert.Query().Where(giveawayalert.AwardIDEQ(alertData.awardID), giveawayalert.CategoryEQ("billing-reconciliation")).Only(ctx)
	if ent.IsNotFound(err) {
		_, err = alertData.db.GiveawayAlert.Create().SetID(alertData.awardID + ":billing-reconciliation").SetAwardID(alertData.awardID).SetOperationID(alertData.operationID).SetCategory("billing-reconciliation").SetState("unresolved").SetMessage(alertData.message).SetLastSeenAt(alertData.now).Save(ctx)
		return err
	}
	if err != nil {
		return err
	}
	_, err = alert.Update().SetState("unresolved").SetMessage(alertData.message).SetLastSeenAt(alertData.now).Save(ctx)
	return err
}
