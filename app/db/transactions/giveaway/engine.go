// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package giveaway

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strconv"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/billingoperation"
	"ItsBagelBot/app/db/transactions/ent/giveawayalert"
	"ItsBagelBot/app/db/transactions/ent/giveawayaward"
	"ItsBagelBot/app/db/transactions/ent/giveawayfulfillmentplan"
	"ItsBagelBot/app/db/transactions/ent/giveawayoutbox"
	"ItsBagelBot/app/db/transactions/ent/giveawayuserlease"
	"ItsBagelBot/app/db/transactions/tebex"
	giveawaysrpc "ItsBagelBot/internal/domain/rpc/giveaways"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/codec"
)

type UsersPort interface {
	Pool(context.Context, *time.Time) (usersrpc.GiveawayPoolReply, error)
	Coverage(context.Context, uint64) (usersrpc.PremiumCoverage, error)
	Prepare(context.Context, usersrpc.PreparePremiumGrantRequest) (usersrpc.PremiumGrant, error)
	Commit(context.Context, usersrpc.CommitPremiumGrantRequest) (usersrpc.PremiumGrant, error)
	Email(context.Context, uint64) (string, error)
}

type EngineConfig struct {
	Store    *Store
	Users    UsersPort
	Mailer   GiveawayMailer
	Provider tebex.RecurringProvider
	Config   Config
	Now      func() time.Time
}

type Engine struct {
	store    *Store
	users    UsersPort
	mailer   GiveawayMailer
	provider tebex.RecurringProvider
	config   Config
	now      func() time.Time
	mu       sync.Mutex
}

func (e *Engine) Store() *Store { return e.store }

func NewEngine(cfg EngineConfig) *Engine {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Engine{store: cfg.Store, users: cfg.Users, mailer: cfg.Mailer, provider: cfg.Provider, config: cfg.Config, now: now}
}

func (e *Engine) Run(ctx context.Context) error {
	interval := e.config.BoundaryInterval
	if interval <= 0 {
		interval = e.config.ReconcileInterval
	}
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		e.dispatchBatch(ctx, 32)
		_ = e.reconcileLifecycle(ctx)
		_ = e.reconcileProviders(ctx)
		_ = e.reconcileEmails(ctx)
		if err := waitForTick(ctx, ticker); err != nil {
			return err
		}
	}
}

func (e *Engine) dispatchBatch(ctx context.Context, limit int) {
	for i := 0; i < limit; i++ {
		if err := e.DispatchOnce(ctx); ent.IsNotFound(err) {
			return
		}
	}
}

func waitForTick(ctx context.Context, ticker *time.Ticker) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ticker.C:
		return nil
	}
}

func (e *Engine) Retry(ctx context.Context, awardID string) error {
	return e.store.RetryAward(ctx, awardID, e.now())
}

// The claim lease must be written before any Users, Tebex or Resend call.
func (e *Engine) DispatchOnce(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	ready := giveawayoutbox.Or(giveawayoutbox.NextAttemptAtIsNil(), giveawayoutbox.NextAttemptAtLTE(now))
	row, err := e.store.DB.GiveawayOutbox.Query().Where(ready, giveawayoutbox.Or(giveawayoutbox.StateIn("queued", "retry"), giveawayoutbox.And(giveawayoutbox.StateEQ("processing"), giveawayoutbox.LeaseUntilLT(now)))).Order(ent.Asc(giveawayoutbox.FieldCreatedAt)).First(ctx)
	if err != nil {
		return err
	}
	lease := e.now().Add(2 * time.Minute)
	owner := uuid.NewString()
	claim := row.Update().Where(giveawayoutbox.Or(giveawayoutbox.StateIn("queued", "retry"), giveawayoutbox.And(giveawayoutbox.StateEQ("processing"), giveawayoutbox.LeaseUntilLT(now)))).SetState("processing").SetLeaseUntil(lease).SetAttempts(row.Attempts + 1).SetUpdatedAt(e.now())
	claim.SetLeaseOwner(owner)
	claimed, err := claim.Save(ctx)
	if err != nil {
		return err
	}
	var payload struct {
		AwardID string `json:"award_id"`
	}
	if err := codec.Unmarshal([]byte(claimed.PayloadJSON), &payload); err != nil {
		return e.failOutbox(ctx, claimed.ID, owner, err)
	}
	var processErr error
	switch claimed.EventType {
	case "award.fulfill":
		processErr = e.fulfill(ctx, payload.AwardID)
	case "award.email.selection":
		processErr = e.awardEmail(ctx, payload.AwardID, "selection")
	case "award.email.confirmation":
		processErr = e.awardEmail(ctx, payload.AwardID, "confirmation")
	default:
		processErr = fmt.Errorf("unknown giveaway event %q", claimed.EventType)
	}
	if processErr != nil {
		return e.failOutbox(ctx, claimed.ID, owner, processErr)
	}
	_ = e.reconcileLifecycle(ctx)
	_, err = claimed.Update().Where(giveawayoutbox.LeaseOwnerEQ(owner)).SetState("completed").ClearLeaseUntil().SetLeaseOwner("").ClearNextAttemptAt().SetUpdatedAt(e.now()).Save(ctx)
	return err
}

func (e *Engine) failOutbox(ctx context.Context, id, owner string, cause error) error {
	row, findErr := e.store.DB.GiveawayOutbox.Get(ctx, id)
	if findErr != nil {
		return findErr
	}
	backoff := time.Minute * time.Duration(1<<min(row.Attempts, 6))
	next := e.now().Add(backoff)
	_, err := e.store.DB.GiveawayOutbox.UpdateOneID(id).Where(giveawayoutbox.LeaseOwnerEQ(owner)).SetState("retry").SetLastError(cause.Error()).ClearLeaseUntil().SetLeaseOwner("").SetNextAttemptAt(next).SetUpdatedAt(e.now()).Save(ctx)
	if err != nil {
		return err
	}
	return cause
}

func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func (e *Engine) fulfill(ctx context.Context, awardID string) error {
	award, err := e.fulfillmentAward(ctx, awardID)
	if err != nil || award == nil {
		return err
	}
	adapter := &engineAwardAdapter{db: e.store.DB, leaseEnabled: true}
	release, leaseErr := adapter.AcquireUserLease(ctx, strconv.FormatUint(award.UserID, 10))
	if leaseErr != nil {
		return leaseErr
	}
	defer release()
	return e.fulfillAward(ctx, awardID, award, adapter)
}

func (e *Engine) fulfillAward(ctx context.Context, awardID string, award *ent.GiveawayAward, adapter *engineAwardAdapter) error {
	workCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	coverage, err := e.users.Coverage(workCtx, award.UserID)
	if err != nil {
		return err
	}
	plan, err := e.planAward(workCtx, award, coverage)
	if err != nil {
		return err
	}
	if award, err = e.saveFulfillmentPlan(ctx, award, plan); err != nil {
		return err
	}
	ref, err := e.prepareBilling(workCtx, billingPlan{award: award, coverage: coverage, start: plan.start, end: plan.end})
	if err != nil {
		return err
	}
	adapter.required, adapter.ref, adapter.leaseEnabled = ref != "" || coverage.BillingUncertain, ref, false
	return e.runWorker(workCtx, adapter, awardID)
}

func (e *Engine) fulfillmentAward(ctx context.Context, awardID string) (*ent.GiveawayAward, error) {
	if err := e.requireUsers(ctx, awardID); err != nil {
		return nil, err
	}
	award, err := e.store.DB.GiveawayAward.Get(ctx, awardID)
	if err != nil {
		return nil, err
	}
	if terminalAward(award) {
		return nil, nil
	}
	return award, nil
}

func (e *Engine) runWorker(ctx context.Context, adapter *engineAwardAdapter, awardID string) error {
	var provider ProtectionProvider
	if e.provider != nil {
		provider = engineProviderAdapter{provider: e.provider, db: e.store.DB, awardID: awardID}
	}
	worker := &Worker{Awards: adapter, Grants: engineGrantAdapter{users: e.users}, Provider: provider, ProviderMutations: e.config.CanMutateProvider()}
	return worker.Fulfill(ctx, awardID)
}

func (e *Engine) requireUsers(ctx context.Context, awardID string) error {
	if e.users != nil {
		return nil
	}
	_ = e.alert(ctx, awardID, "fulfillment", "users giveaway contract unavailable")
	return errors.New("users giveaway contract unavailable")
}

func terminalAward(award *ent.GiveawayAward) bool {
	return award.State == string(giveawaysrpc.AwardScheduled) || award.State == string(giveawaysrpc.AwardActive) || award.State == string(giveawaysrpc.AwardCompleted) || award.State == string(giveawaysrpc.AwardVoided)
}

func (e *Engine) needsReview(ctx context.Context, award *ent.GiveawayAward, reason any) error {
	_, err := award.Update().SetState(string(giveawaysrpc.AwardNeedsReview)).SetBillingState(string(giveawaysrpc.BillingPending)).SetFailureReason(fmt.Sprint(reason)).SetVersion(award.Version + 1).SetUpdatedAt(e.now()).Save(ctx)
	if err != nil {
		return err
	}
	err = e.alert(ctx, award.ID, "fulfillment", fmt.Sprint(reason))
	if err != nil {
		return err
	}
	return ErrAwardNeedsReview
}

func (e *Engine) alert(ctx context.Context, awardID, category, message string) error {
	row, findErr := e.store.DB.GiveawayAlert.Query().Where(giveawayalert.AwardIDEQ(awardID), giveawayalert.CategoryEQ(category)).Only(ctx)
	if findErr == nil {
		_, err := row.Update().SetState("unresolved").SetMessage(message).SetLastSeenAt(e.now()).Save(ctx)
		return err
	}
	if !ent.IsNotFound(findErr) {
		return findErr
	}
	_, err := e.store.DB.GiveawayAlert.Create().SetID(awardID + ":" + category).SetAwardID(awardID).SetCategory(category).SetState("unresolved").SetMessage(message).SetLastSeenAt(e.now()).Save(ctx)
	if ent.IsConstraintError(err) {
		row, findErr = e.store.DB.GiveawayAlert.Query().Where(giveawayalert.AwardIDEQ(awardID), giveawayalert.CategoryEQ(category)).Only(ctx)
		if findErr != nil {
			return findErr
		}
		_, err = row.Update().SetState("unresolved").SetMessage(message).SetLastSeenAt(e.now()).Save(ctx)
	}
	return err
}

func (e *Engine) reconcileLifecycle(ctx context.Context) error {
	rows, err := e.store.DB.GiveawayAward.Query().Where(giveawayaward.StateIn(string(giveawaysrpc.AwardScheduled), string(giveawaysrpc.AwardActive))).All(ctx)
	if err != nil {
		return err
	}
	now := e.now()
	for _, row := range rows {
		if state := lifecycleState(row, now); state != "" {
			if _, err = row.Update().SetState(state).SetUpdatedAt(now).Save(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func lifecycleState(row *ent.GiveawayAward, now time.Time) string {
	if lifecycleComplete(row, now) {
		return string(giveawaysrpc.AwardCompleted)
	}
	if lifecycleActive(row, now) {
		return string(giveawaysrpc.AwardActive)
	}
	return ""
}

func lifecycleComplete(row *ent.GiveawayAward, now time.Time) bool {
	return !row.PlannedEnd.IsZero() && !now.Before(row.PlannedEnd)
}

func lifecycleActive(row *ent.GiveawayAward, now time.Time) bool {
	return !row.PlannedStart.IsZero() && !now.Before(row.PlannedStart) && row.State != string(giveawaysrpc.AwardActive)
}

type engineAwardAdapter struct {
	db           *ent.Client
	required     bool
	ref          string
	leaseEnabled bool
}

func (a *engineAwardAdapter) Load(ctx context.Context, id string) (WorkAward, error) {
	row, err := a.db.GiveawayAward.Get(ctx, id)
	if err != nil {
		return WorkAward{}, err
	}
	plan, err := a.db.GiveawayFulfillmentPlan.Query().Where(giveawayfulfillmentplan.AwardIDEQ(id)).Only(ctx)
	if err != nil {
		return WorkAward{}, err
	}
	return WorkAward{ID: row.ID, GiveawayID: row.GiveawayID, UserID: strconv.FormatUint(row.UserID, 10), Start: valueTime(plan.StartAt), End: valueTime(plan.EndAt), BillingRequired: a.required, RecurringReference: a.ref, IntervalRule: plan.IntervalRule}, nil
}
func (a *engineAwardAdapter) Preparing(ctx context.Context, id string) error {
	_, err := a.db.GiveawayAward.UpdateOneID(id).SetState(string(giveawaysrpc.AwardPreparing)).SetUpdatedAt(time.Now()).Save(ctx)
	return err
}
func (a *engineAwardAdapter) NeedsReview(ctx context.Context, id string, reason error) error {
	_, err := a.db.GiveawayAward.UpdateOneID(id).SetState(string(giveawaysrpc.AwardNeedsReview)).SetFailureReason(reason.Error()).Save(ctx)
	if opErr := markBillingReview(ctx, a.db, id, reason); err == nil {
		err = opErr
	}
	if alertErr := persistReviewAlert(ctx, a.db, id, reason); err == nil {
		err = alertErr
	}
	return err
}
func (a *engineAwardAdapter) AcquireUserLease(ctx context.Context, userID string) (func(), error) {
	if !a.leaseEnabled {
		return func() {}, nil
	}
	uid, err := strconv.ParseUint(userID, 10, 64)
	if err != nil || uid == 0 {
		return nil, errors.New("invalid user id")
	}
	now := time.Now()
	owner := uuid.NewString()
	until := now.Add(2 * time.Minute)
	tx, err := a.db.Tx(ctx)
	if err != nil {
		return nil, err
	}
	request := userLeaseRequest{id: userID, uid: uid, owner: owner, until: until, now: now}
	row, findErr := lockUserLease(ctx, tx, uid)
	err = request.save(ctx, tx, row, findErr)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return func() {
		_, _ = a.db.GiveawayUserLease.UpdateOneID("user:" + userID).Where(giveawayuserlease.OwnerEQ(owner)).SetLeaseUntil(time.Now()).SetUpdatedAt(time.Now()).Save(context.Background())
	}, nil
}

type userLeaseRequest struct {
	id, owner  string
	uid        uint64
	until, now time.Time
}

func lockUserLease(ctx context.Context, tx *ent.Tx, userID uint64) (*ent.GiveawayUserLease, error) {
	row, err := tx.GiveawayUserLease.Query().Where(giveawayuserlease.UserIDEQ(userID)).ForUpdate().Only(ctx)
	if err != nil && strings.Contains(err.Error(), "FOR UPDATE/SHARE not supported") {
		return tx.GiveawayUserLease.Query().Where(giveawayuserlease.UserIDEQ(userID)).Only(ctx)
	}
	return row, err
}

func (request userLeaseRequest) save(ctx context.Context, tx *ent.Tx, row *ent.GiveawayUserLease, findErr error) error {
	if findErr == nil {
		if row.LeaseUntil.After(request.now) {
			return errors.New("user fulfillment already leased")
		}
		_, err := row.Update().SetOwner(request.owner).SetLeaseUntil(request.until).SetUpdatedAt(request.now).Save(ctx)
		return err
	}
	if !ent.IsNotFound(findErr) {
		return findErr
	}
	_, err := tx.GiveawayUserLease.Create().SetID("user:" + request.id).SetUserID(request.uid).SetOwner(request.owner).SetLeaseUntil(request.until).SetUpdatedAt(request.now).Save(ctx)
	return err
}
func (a *engineAwardAdapter) Scheduled(ctx context.Context, id, grantID string) error {
	tx, err := a.db.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = a.scheduleInTx(ctx, tx, id, grantID); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *engineAwardAdapter) scheduleInTx(ctx context.Context, tx *ent.Tx, id, grantID string) error {
	award, err := tx.GiveawayAward.Get(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now()
	billing := string(giveawaysrpc.BillingProtected)
	if !a.required {
		billing = string(giveawaysrpc.BillingNotRequired)
	}
	update := award.Update().SetState(string(giveawaysrpc.AwardScheduled)).SetBillingState(billing).SetGrantID(grantID).SetUpdatedAt(now).ClearFailureReason()
	setConfirmedPeriod(update, award)
	if _, err = update.Save(ctx); err != nil {
		return err
	}
	if _, err = tx.GiveawayAlert.Update().Where(giveawayalert.AwardIDEQ(id), giveawayalert.CategoryEQ("fulfillment")).SetState("resolved").SetResolvedAt(now).SetLastSeenAt(now).Save(ctx); err != nil {
		return err
	}
	if a.required {
		_, err = tx.BillingOperation.Update().Where(billingoperation.AwardIDEQ(id)).SetState("verified").SetVerifiedAt(now).SetUpdatedAt(now).Save(ctx)
	}
	return err
}

func setConfirmedPeriod(update *ent.GiveawayAwardUpdateOne, award *ent.GiveawayAward) {
	if !award.PlannedStart.IsZero() {
		update.SetConfirmedStart(award.PlannedStart)
	}
	if !award.PlannedEnd.IsZero() {
		update.SetConfirmedEnd(award.PlannedEnd)
	}
}

func markBillingReview(ctx context.Context, db *ent.Client, awardID string, reason error) error {
	_, err := db.BillingOperation.Update().Where(billingoperation.AwardIDEQ(awardID)).SetState("needs_review").SetLastError(reason.Error()).SetUpdatedAt(time.Now()).Save(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

func persistReviewAlert(ctx context.Context, db *ent.Client, awardID string, reason error) error {
	now := time.Now()
	alert, err := db.GiveawayAlert.Query().Where(giveawayalert.AwardIDEQ(awardID), giveawayalert.CategoryEQ("billing")).Only(ctx)
	if err == nil {
		return reopenReviewAlert(ctx, alert, reason, now)
	}
	if !ent.IsNotFound(err) {
		return err
	}
	_, err = db.GiveawayAlert.Create().SetID(awardID + ":billing").SetAwardID(awardID).SetCategory("billing").SetState("unresolved").SetMessage(reason.Error()).SetLastSeenAt(now).Save(ctx)
	if !ent.IsConstraintError(err) {
		return err
	}
	alert, err = db.GiveawayAlert.Query().Where(giveawayalert.AwardIDEQ(awardID), giveawayalert.CategoryEQ("billing")).Only(ctx)
	if err != nil {
		return err
	}
	return reopenReviewAlert(ctx, alert, reason, now)
}

func reopenReviewAlert(ctx context.Context, alert *ent.GiveawayAlert, reason error, now time.Time) error {
	_, err := alert.Update().SetState("unresolved").SetMessage(reason.Error()).SetLastSeenAt(now).ClearAcknowledgedBy().ClearAcknowledgedAt().ClearResolvedAt().Save(ctx)
	return err
}
func valueTime(v time.Time) time.Time {
	return v.UTC()
}

type engineGrantAdapter struct{ users UsersPort }

func (a engineGrantAdapter) Prepare(ctx context.Context, award WorkAward) (string, error) {
	uid, _ := strconv.ParseUint(award.UserID, 10, 64)
	grant, err := a.users.Prepare(ctx, usersrpc.PreparePremiumGrantRequest{GiveawayID: award.GiveawayID, AwardID: award.ID, UserID: uid, StartAt: award.Start, EndAt: award.End, IntervalRuleVersion: award.IntervalRule})
	if err != nil {
		return "", err
	}
	return strconv.Itoa(grant.ID), nil
}
func (a engineGrantAdapter) Commit(ctx context.Context, award WorkAward, _ string) error {
	uid, _ := strconv.ParseUint(award.UserID, 10, 64)
	_, err := a.users.Commit(ctx, usersrpc.CommitPremiumGrantRequest{GiveawayID: award.GiveawayID, AwardID: award.ID, UserID: uid})
	return err
}

type engineProviderAdapter struct {
	provider tebex.RecurringProvider
	db       *ent.Client
	awardID  string
}

func (a engineProviderAdapter) Inspect(ctx context.Context, ref string) (ProviderState, error) {
	payment, err := a.provider.GetRecurring(ctx, ref)
	if err != nil {
		return ProviderState{}, err
	}
	state := providerState(payment, ref)
	if state.ProtectedUntil != nil && !a.protectionEvidence(ctx, ref) {
		state.ProtectedUntil = nil
		state.Ambiguous = true
	}
	return state, nil
}
func (a engineProviderAdapter) Protect(ctx context.Context, ref string, until time.Time) (ProviderState, error) {
	if err := a.markOperation(ctx, "protecting", ""); err != nil {
		return ProviderState{}, err
	}
	payment, err := a.provider.PauseRecurring(ctx, ref, until)
	if err != nil {
		_ = a.markOperation(ctx, "uncertain", err.Error())
		return ProviderState{}, err
	}
	if err := a.markSnapshot(ctx, payment); err != nil {
		return ProviderState{}, err
	}
	return providerState(payment, ref), nil
}

func (a engineProviderAdapter) markOperation(ctx context.Context, state, lastError string) error {
	if a.db == nil || a.awardID == "" {
		return nil
	}
	operation, err := a.db.BillingOperation.Query().Where(billingoperation.AwardIDEQ(a.awardID)).Only(ctx)
	if err != nil {
		return err
	}
	update := operation.Update().SetState(state).SetAttempts(operation.Attempts + 1).SetUpdatedAt(time.Now())
	if lastError != "" {
		update.SetLastError(lastError)
	}
	_, err = update.Save(ctx)
	return err
}

func (a engineProviderAdapter) markSnapshot(ctx context.Context, payment tebex.RecurringPayment) error {
	if a.db == nil || a.awardID == "" {
		return nil
	}
	snapshot, err := codec.Marshal(payment)
	if err != nil {
		return err
	}
	_, err = a.db.BillingOperation.Update().Where(billingoperation.AwardIDEQ(a.awardID)).SetAfterSnapshotJSON(string(snapshot)).SetUpdatedAt(time.Now()).Save(ctx)
	return err
}

func (a engineProviderAdapter) protectionEvidence(ctx context.Context, reference string) bool {
	if a.db == nil || a.awardID == "" {
		return false
	}
	operation, err := a.db.BillingOperation.Query().Where(billingoperation.AwardIDEQ(a.awardID)).Only(ctx)
	if err != nil || operation.RecurringReference != reference {
		return false
	}
	return operation.State == "protecting" || operation.State == "uncertain" || operation.State == "verified"
}

// A response not proven to match the requested subscription must never authorize a prize.
func providerState(payment tebex.RecurringPayment, ref string) ProviderState {
	state := ProviderState{
		Reference:             ref,
		CancellationRequested: paymentHasCancellation(payment),
		Ambiguous:             paymentIsAmbiguous(payment, ref),
	}
	state.ProtectedUntil = providerProtectedUntil(payment, state.Ambiguous)
	return state
}

func paymentHasCancellation(payment tebex.RecurringPayment) bool {
	return payment.CancellationRequested || payment.Cancelled || payment.CancellationDate != nil
}

func paymentIsAmbiguous(payment tebex.RecurringPayment, reference string) bool {
	return payment.Ambiguous || !payment.CanProtect(reference)
}

func providerProtectedUntil(payment tebex.RecurringPayment, ambiguous bool) *time.Time {
	if ambiguous || payment.Status != "Paused" {
		return nil
	}
	return payment.PausedUntil
}
