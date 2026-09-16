// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/adminuser"
	"ItsBagelBot/app/db/users/ent/premiumgrant"
	"ItsBagelBot/app/db/users/ent/user"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/db"
)

const giveawayRuleVersion = "eligibility-v1"

// GiveawayPoolCounts are mutually exclusive exclusion counts. The order is
// deliberate: moderation and account classification take precedence over
// tier, so the preview can explain every registered row exactly once.
type GiveawayPoolCounts struct {
	Total        int `json:"total"`
	Eligible     int `json:"eligible"`
	Banned       int `json:"banned"`
	Inactive     int `json:"inactive"`
	NotOnboarded int `json:"not_onboarded"`
	TestAccount  int `json:"test_account"`
	VIP          int `json:"vip"`
	CurrentStaff int `json:"current_staff"`
}

// GiveawayPool is the complete snapshot consumed by Transactions. It is
// intentionally unpaged; a caller cannot accidentally draw from a loaded
// admin-directory page.
type GiveawayPool struct {
	Candidates  []usersrpc.GiveawayCandidate `json:"candidates"`
	SnapshotAt  time.Time                    `json:"snapshot_at"`
	RuleVersion string                       `json:"rule_version"`
	Counts      GiveawayPoolCounts           `json:"counts"`
}

type pendingGiveawayPrefs struct {
	active    *bool
	onboarded *bool
}

func (r *Users) preferenceSnapshot() map[uint64]pendingGiveawayPrefs {
	r.pendingMu.RLock()
	defer r.pendingMu.RUnlock()
	result := make(map[uint64]pendingGiveawayPrefs)
	for key, value := range r.pendingPrefs {
		pending := result[key.userID]
		switch key.field {
		case prefActive:
			v := value.flag
			pending.active = &v
		case prefOnboarded:
			v := value.flag
			pending.onboarded = &v
		}
		result[key.userID] = pending
	}
	return result
}

// EligibleGiveawayPool returns only the candidates for callers that do not
// need preview counts. The full snapshot API is GiveawayPoolSnapshot.
func (r *Users) EligibleGiveawayPool(ctx context.Context, createdBefore *time.Time) ([]usersrpc.GiveawayCandidate, error) {
	p, err := r.GiveawayPoolSnapshot(ctx, createdBefore)
	if err != nil {
		return nil, err
	}
	return p.Candidates, nil
}

// GiveawayPoolSnapshot resolves eligibility from the authoritative Users
// tables. Active staff is checked by roster membership, so disabled former
// staff rows remain eligible. Pending write-behind active/onboarding values
// are overlaid by the repository in preferenceSnapshot.
func (r *Users) GiveawayPoolSnapshot(ctx context.Context, createdBefore *time.Time) (GiveawayPool, error) {
	rows, err := r.giveawayUsers(ctx, createdBefore)
	if err != nil {
		return GiveawayPool{}, err
	}
	staffIDs, err := r.activeStaffIDs(ctx)
	if err != nil {
		return GiveawayPool{}, err
	}
	pool := GiveawayPool{SnapshotAt: time.Now().UTC(), RuleVersion: giveawayRuleVersion}
	pref := r.preferenceSnapshot()
	for _, row := range rows {
		active, onboarded := row.IsActive, row.Onboarded
		if p, ok := pref[row.ID]; ok {
			active, onboarded = pendingEligibility(p, active, onboarded)
		}
		pool.Counts.Total++
		pool.add(row, active, onboarded, hasActiveStaff(staffIDs, row.ID))
	}
	return pool, nil
}

func (r *Users) giveawayUsers(ctx context.Context, createdBefore *time.Time) ([]*ent.User, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
		q := r.client.User.Query().Order(ent.Asc(user.FieldID))
		if createdBefore != nil {
			q = q.Where(user.CreatedAtLTE(*createdBefore))
		}
		return q.All(ctx)
	})
}

func (r *Users) activeStaffIDs(ctx context.Context) (map[uint64]struct{}, error) {
	staff, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.AdminUser, error) {
		return r.client.AdminUser.Query().Where(adminuser.ActiveEQ(true)).Select(adminuser.FieldID).All(ctx)
	})
	if err != nil {
		return nil, err
	}
	ids := make(map[uint64]struct{}, len(staff))
	for _, s := range staff {
		ids[s.ID] = struct{}{}
	}
	return ids, nil
}

func pendingEligibility(p pendingGiveawayPrefs, active, onboarded bool) (bool, bool) {
	if p.active != nil {
		active = *p.active
	}
	if p.onboarded != nil {
		onboarded = *p.onboarded
	}
	return active, onboarded
}

func (p *GiveawayPool) add(row *ent.User, active, onboarded, staff bool) {
	switch {
	case row.Banned:
		p.Counts.Banned++
	case !active:
		p.Counts.Inactive++
	case !onboarded:
		p.Counts.NotOnboarded++
	case row.TestAccount:
		p.Counts.TestAccount++
	case row.Status == user.StatusVip:
		p.Counts.VIP++
	case staff:
		p.Counts.CurrentStaff++
	default:
		p.Counts.Eligible++
		p.Candidates = append(p.Candidates, candidateView(row, active, onboarded))
	}
}

func candidateView(row *ent.User, active, onboarded bool) usersrpc.GiveawayCandidate {
	return usersrpc.GiveawayCandidate{UserID: row.ID, Username: row.Username, DisplayName: row.DisplayName,
		ContactEmailAvailable: len(row.EmailEnc) != 0, IsActive: active, Onboarded: onboarded,
		Status: string(row.Status), SubscriptionSource: row.SubscriptionSource,
		SubscriptionExpiresAt: row.SubscriptionExpiresAt, SubscriptionRef: row.SubscriptionRef,
		SubscriptionCancelPending: row.SubscriptionCancelPending}
}

func hasActiveStaff(staff map[uint64]struct{}, id uint64) bool {
	_, ok := staff[id]
	return ok
}

// SetTestAccount is an authorized, audited mutation. Moderators cannot alter
// the marker because doing so would change the authoritative draw population.
func (r *Users) SetTestAccount(ctx context.Context, targetID uint64, enabled bool, actorID uint64) error {
	if err := validate.UserID(targetID); err != nil {
		return err
	}
	if err := validate.UserID(actorID); err != nil {
		return err
	}
	return withTx(ctx, r.client, func(tx *ent.Tx) error {
		actor, err := tx.AdminUser.Query().Where(
			adminuser.IDEQ(actorID), adminuser.ActiveEQ(true),
			adminuser.RoleIn(adminuser.RoleAdmin, adminuser.RoleOwner),
		).Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return errors.New("test-account marker requires an active admin")
			}
			return err
		}
		if _, err := tx.User.UpdateOneID(targetID).SetTestAccount(enabled).Save(ctx); err != nil {
			return err
		}
		_, err = tx.AdminAudit.Create().
			SetActorID(actor.ID).SetActorLogin(actor.Login).
			SetAction("set_test_account").SetTarget(fmt.Sprint(targetID)).
			SetDetail(fmt.Sprintf("enabled=%t", enabled)).SetOk(true).Save(ctx)
		return err
	})
}

func validateGrantRequest(req usersrpc.PreparePremiumGrantRequest) error {
	if err := validate.UserID(req.UserID); err != nil {
		return err
	}
	if missingGrantIdentity(req) {
		return errors.New("giveaway_id and award_id are required")
	}
	if invalidGrantInterval(req) {
		return errors.New("premium grant interval must be non-empty")
	}
	if !mysqlDateTime(req.StartAt) || !mysqlDateTime(req.EndAt) {
		return errors.New("premium grant dates must be between years 1000 and 9999")
	}
	if strings.TrimSpace(req.IntervalRuleVersion) == "" {
		return errors.New("interval rule version is required")
	}
	return nil
}

func normalizeGrantRequest(req usersrpc.PreparePremiumGrantRequest) usersrpc.PreparePremiumGrantRequest {
	req.GiveawayID = strings.TrimSpace(req.GiveawayID)
	req.AwardID = strings.TrimSpace(req.AwardID)
	req.IntervalRuleVersion = strings.TrimSpace(req.IntervalRuleVersion)
	req.StartAt = req.StartAt.UTC().Truncate(time.Microsecond)
	req.EndAt = req.EndAt.UTC().Truncate(time.Microsecond)
	return req
}

func mysqlDateTime(value time.Time) bool {
	return value.Year() >= 1000 && value.Year() <= 9999
}

func missingGrantIdentity(req usersrpc.PreparePremiumGrantRequest) bool {
	return strings.TrimSpace(req.GiveawayID) == "" || strings.TrimSpace(req.AwardID) == ""
}

func invalidGrantInterval(req usersrpc.PreparePremiumGrantRequest) bool {
	return req.StartAt.IsZero() || req.EndAt.IsZero() || !req.EndAt.After(req.StartAt)
}

func grantView(g *ent.PremiumGrant) usersrpc.PremiumGrant {
	return usersrpc.PremiumGrant{ID: g.ID, GiveawayID: g.GiveawayID, AwardID: g.AwardID,
		UserID: g.UserID, State: string(g.State), StartAt: g.StartAt, EndAt: g.EndAt,
		IntervalRuleVersion: g.IntervalRuleVersion}
}

func (r *Users) findGrant(ctx context.Context, giveawayID, awardID string, userID uint64) (*ent.PremiumGrant, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (*ent.PremiumGrant, error) {
		return r.client.PremiumGrant.Query().Where(
			premiumgrant.GiveawayIDEQ(giveawayID), premiumgrant.AwardIDEQ(awardID), premiumgrant.UserIDEQ(userID),
		).Only(ctx)
	})
}

// PreparePremiumGrant durably reserves one award interval. The unique award
// identity makes retries and concurrent delivery attempts converge to one row.
func (r *Users) PreparePremiumGrant(ctx context.Context, req usersrpc.PreparePremiumGrantRequest) (usersrpc.PremiumGrant, error) {
	req = normalizeGrantRequest(req)
	if err := validateGrantRequest(req); err != nil {
		return usersrpc.PremiumGrant{}, err
	}
	if _, err := r.FindUser(ctx, req.UserID); err != nil {
		return usersrpc.PremiumGrant{}, err
	}
	row, err := r.prepareGrantTx(ctx, req)
	if err == nil {
		return grantView(row), nil
	}
	if !ent.IsConstraintError(err) {
		return usersrpc.PremiumGrant{}, err
	}
	row, lookupErr := r.findGrant(ctx, strings.TrimSpace(req.GiveawayID), strings.TrimSpace(req.AwardID), req.UserID)
	if lookupErr != nil {
		return usersrpc.PremiumGrant{}, lookupErr
	}
	if !grantIntervalMatches(row, req) {
		return usersrpc.PremiumGrant{}, errors.New("award identity already prepared with different interval")
	}
	return grantView(row), nil
}

// prepareGrantTx serializes reservations for one account and rejects a
// different award whose interval overlaps existing committed or prepared
// coverage. The stable identity replay is checked first so retries remain
// idempotent while two campaigns cannot consume the same entitlement window.
func (r *Users) prepareGrantTx(ctx context.Context, req usersrpc.PreparePremiumGrantRequest) (*ent.PremiumGrant, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if lockErr := lockGrantUser(ctx, tx, req.UserID); lockErr != nil {
		return nil, lockErr
	}
	existing, err := overlappingGrants(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	for _, row := range existing {
		return reuseOrRejectGrant(tx, row, req)
	}
	row, err := insertGrant(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

func lockGrantUser(ctx context.Context, tx *ent.Tx, id uint64) error {
	_, err := tx.User.Query().Where(user.IDEQ(id)).ForUpdate().Only(ctx)
	if err != nil && err.Error() == "sql: SELECT .. FOR UPDATE/SHARE not supported in SQLite" {
		return nil
	}
	return err
}

func overlappingGrants(ctx context.Context, tx *ent.Tx, req usersrpc.PreparePremiumGrantRequest) ([]*ent.PremiumGrant, error) {
	return tx.PremiumGrant.Query().Where(
		premiumgrant.UserIDEQ(req.UserID), premiumgrant.StateIn(premiumgrant.StatePrepared, premiumgrant.StateCommitted),
		premiumgrant.StartAtLT(req.EndAt.UTC()), premiumgrant.EndAtGT(req.StartAt.UTC()),
	).Order(ent.Asc(premiumgrant.FieldStartAt), ent.Asc(premiumgrant.FieldID)).All(ctx)
}

func reuseOrRejectGrant(tx *ent.Tx, row *ent.PremiumGrant, req usersrpc.PreparePremiumGrantRequest) (*ent.PremiumGrant, error) {
	if row.GiveawayID != strings.TrimSpace(req.GiveawayID) || row.AwardID != strings.TrimSpace(req.AwardID) {
		return nil, errors.New("award interval overlaps existing premium coverage")
	}
	if !grantIntervalMatches(row, req) {
		return nil, errors.New("award identity already prepared with different interval")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

func insertGrant(ctx context.Context, tx *ent.Tx, req usersrpc.PreparePremiumGrantRequest) (*ent.PremiumGrant, error) {
	return tx.PremiumGrant.Create().SetUserID(req.UserID).
		SetGiveawayID(strings.TrimSpace(req.GiveawayID)).SetAwardID(strings.TrimSpace(req.AwardID)).
		SetStartAt(req.StartAt.UTC()).SetEndAt(req.EndAt.UTC()).
		SetIntervalRuleVersion(strings.TrimSpace(req.IntervalRuleVersion)).Save(ctx)
}

func grantIntervalMatches(row *ent.PremiumGrant, req usersrpc.PreparePremiumGrantRequest) bool {
	return row.StartAt.Equal(req.StartAt.UTC()) && row.EndAt.Equal(req.EndAt.UTC()) &&
		row.IntervalRuleVersion == strings.TrimSpace(req.IntervalRuleVersion)
}

// CommitPremiumGrant transitions prepared to committed and is idempotent for
// an already committed award. It never changes the interval or billing
// identity. Access status is projected into users.status so admin stats and
// listing read the same paid column dashboard Get does.
func (r *Users) CommitPremiumGrant(ctx context.Context, req usersrpc.CommitPremiumGrantRequest) (usersrpc.PremiumGrant, error) {
	if err := validate.UserID(req.UserID); err != nil {
		return usersrpc.PremiumGrant{}, err
	}
	row, err := r.findGrant(ctx, strings.TrimSpace(req.GiveawayID), strings.TrimSpace(req.AwardID), req.UserID)
	if err != nil {
		return usersrpc.PremiumGrant{}, err
	}
	if row.State == premiumgrant.StateCommitted {
		return r.reannounceCommitted(ctx, row)
	}
	if row.State != premiumgrant.StatePrepared {
		return usersrpc.PremiumGrant{}, fmt.Errorf("cannot commit premium grant in state %q", row.State)
	}
	updated, err := r.commitPrepared(ctx, row.ID)
	if err != nil {
		return usersrpc.PremiumGrant{}, err
	}
	if updated == 0 {
		return r.committedAfterRace(ctx, req)
	}
	row, err = r.findGrant(ctx, req.GiveawayID, req.AwardID, req.UserID)
	if err != nil {
		return usersrpc.PremiumGrant{}, err
	}
	return r.reannounceCommitted(ctx, row)
}

func (r *Users) commitPrepared(ctx context.Context, id int) (int, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.PremiumGrant.Update().Where(
			premiumgrant.IDEQ(id), premiumgrant.StateEQ(premiumgrant.StatePrepared),
		).SetState(premiumgrant.StateCommitted).Save(ctx)
	})
}

func (r *Users) committedAfterRace(ctx context.Context, req usersrpc.CommitPremiumGrantRequest) (usersrpc.PremiumGrant, error) {
	row, err := r.findGrant(ctx, req.GiveawayID, req.AwardID, req.UserID)
	if err != nil {
		return usersrpc.PremiumGrant{}, err
	}
	if row.State != premiumgrant.StateCommitted {
		return usersrpc.PremiumGrant{}, errors.New("premium grant changed concurrently")
	}
	return r.reannounceCommitted(ctx, row)
}

func (r *Users) reannounceCommitted(ctx context.Context, row *ent.PremiumGrant) (usersrpc.PremiumGrant, error) {
	// A prior commit may have succeeded while invalidation failed. Replaying
	// the idempotent commit re-projects access and re-announces it.
	if err := r.projectAccess(ctx, row.UserID, time.Now().UTC()); err != nil {
		return usersrpc.PremiumGrant{}, err
	}
	if err := r.publishChanged(ctx, row.UserID); err != nil {
		return usersrpc.PremiumGrant{}, err
	}
	return grantView(row), nil
}

// CancelPremiumGrant may cancel only an uncommitted reservation. A committed
// award is an owed prize and cannot be silently revoked by a retry or account
// deactivation.
func (r *Users) CancelPremiumGrant(ctx context.Context, req usersrpc.CommitPremiumGrantRequest) error {
	if err := validate.UserID(req.UserID); err != nil {
		return err
	}
	row, err := r.findGrant(ctx, req.GiveawayID, req.AwardID, req.UserID)
	if err != nil {
		return err
	}
	if row.State == premiumgrant.StateCancelled {
		return nil
	}
	if row.State != premiumgrant.StatePrepared {
		return errors.New("committed premium grants cannot be cancelled")
	}
	_, err = r.client.PremiumGrant.UpdateOneID(row.ID).Where(premiumgrant.StateEQ(premiumgrant.StatePrepared)).SetState(premiumgrant.StateCancelled).Save(ctx)
	return err
}

// PremiumCoverage is the read contract used by Transactions to append prize
// months after paid and previously committed giveaway coverage.
func (r *Users) PremiumCoverage(ctx context.Context, userID uint64, now time.Time) (usersrpc.PremiumCoverage, error) {
	if err := validate.UserID(userID); err != nil {
		return usersrpc.PremiumCoverage{}, err
	}
	u, err := r.FindUser(ctx, userID)
	if err != nil {
		return usersrpc.PremiumCoverage{}, err
	}
	grants, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.PremiumGrant, error) {
		return r.client.PremiumGrant.Query().Where(
			premiumgrant.UserIDEQ(userID), premiumgrant.StateEQ(premiumgrant.StateCommitted),
			premiumgrant.EndAtGT(now),
		).Order(ent.Asc(premiumgrant.FieldStartAt), ent.Asc(premiumgrant.FieldID)).All(ctx)
	})
	if err != nil {
		return usersrpc.PremiumCoverage{}, err
	}
	coverage := billingCoverage(u)
	coverage.Grants = make([]usersrpc.PremiumGrant, 0, len(grants))
	for _, grant := range grants {
		coverage.Grants = append(coverage.Grants, grantView(grant))
	}
	return coverage, nil
}

func billingCoverage(u *ent.User) usersrpc.PremiumCoverage {
	coverage := usersrpc.PremiumCoverage{UserID: u.ID, Status: string(u.Status), IsActive: u.IsActive, Banned: u.Banned, TestAccount: u.TestAccount, PaidThrough: u.SubscriptionExpiresAt,
		CancelPending: u.SubscriptionCancelPending, BillingUncertain: uncertainBilling(u)}
	if u.SubscriptionRef != nil {
		ref := *u.SubscriptionRef
		coverage.RecurringReference = &ref
	}
	return coverage
}

func uncertainBilling(u *ent.User) bool {
	if u.Status != user.StatusPaid {
		return false
	}
	if u.SubscriptionSource == "" {
		return true
	}
	if u.SubscriptionSource == "giveaway" {
		return false
	}
	return u.SubscriptionSource == "tebex" && (u.SubscriptionRef == nil || strings.TrimSpace(*u.SubscriptionRef) == "")
}

// ExpirePremiumGrants advances terminal state and projects users.status from
// grant coverage. Tebex/admin billing identity is left alone; a grant-only
// promotion records source "giveaway" so it is not an unknown Tebex agreement.
func (r *Users) ExpirePremiumGrants(ctx context.Context, now time.Time) (int, error) {
	active, err := r.pendingActiveGrants(ctx, now)
	if err != nil {
		return 0, err
	}
	expired, err := r.pendingExpiredGrants(ctx, now)
	if err != nil {
		return 0, err
	}
	if err := r.announceGrantRows(ctx, active, premiumgrant.ProjectionPhaseActive, now); err != nil {
		return 0, err
	}
	count, err := r.expireGrantRows(ctx, expired, now)
	if err != nil {
		return count, err
	}
	if err := r.reconcileGrantAccess(ctx, now); err != nil {
		return count, err
	}
	return count, nil
}

func (r *Users) pendingActiveGrants(ctx context.Context, now time.Time) ([]*ent.PremiumGrant, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.PremiumGrant, error) {
		return r.client.PremiumGrant.Query().Where(premiumgrant.StateEQ(premiumgrant.StateCommitted), premiumgrant.ProjectionPhaseEQ(premiumgrant.ProjectionPhasePending), premiumgrant.StartAtLTE(now), premiumgrant.EndAtGT(now)).All(ctx)
	})
}

func (r *Users) pendingExpiredGrants(ctx context.Context, now time.Time) ([]*ent.PremiumGrant, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.PremiumGrant, error) {
		return r.client.PremiumGrant.Query().Where(premiumgrant.EndAtLTE(now), premiumgrant.Or(premiumgrant.And(premiumgrant.StateEQ(premiumgrant.StateCommitted), premiumgrant.ProjectionPhaseNEQ(premiumgrant.ProjectionPhaseExpired)), premiumgrant.And(premiumgrant.StateEQ(premiumgrant.StateExpired), premiumgrant.ProjectionPhaseNEQ(premiumgrant.ProjectionPhaseExpired)))).All(ctx)
	})
}

func (r *Users) announceGrantRows(ctx context.Context, rows []*ent.PremiumGrant, phase premiumgrant.ProjectionPhase, now time.Time) error {
	for _, row := range rows {
		if err := r.publishGrantPhase(ctx, row, phase, now); err != nil {
			return err
		}
	}
	return nil
}

func (r *Users) expireGrantRows(ctx context.Context, rows []*ent.PremiumGrant, now time.Time) (int, error) {
	count := 0
	for _, row := range rows {
		if row.State == premiumgrant.StateCommitted {
			if _, err := r.client.PremiumGrant.UpdateOneID(row.ID).SetState(premiumgrant.StateExpired).Save(ctx); err != nil {
				return count, err
			}
			count++
		}
		if err := r.publishGrantPhase(ctx, row, premiumgrant.ProjectionPhaseExpired, now); err != nil {
			return count, err
		}
	}
	return count, nil
}

func (r *Users) publishGrantPhase(ctx context.Context, row *ent.PremiumGrant, phase premiumgrant.ProjectionPhase, now time.Time) error {
	if err := r.projectAccess(ctx, row.UserID, now); err != nil {
		return err
	}
	if err := r.publishChanged(ctx, row.UserID); err != nil {
		return err
	}
	if err := r.publishStatusInvalidation(ctx, row.UserID); err != nil {
		return err
	}
	_, err := r.client.PremiumGrant.UpdateOneID(row.ID).SetProjectionPhase(phase).Save(ctx)
	return err
}

// projectAccess writes users.status from grant coverage. VIP is never
// touched. Tebex/admin billing identity stays on its own columns: a covering
// grant promotes free→paid, and a grant-only paid row whose coverage ended
// returns to free. Billing-sourced paid rows are left for ApplyBilling and
// ExpireSubscriptions so Tebex grace is not duplicated here.
func (r *Users) projectAccess(ctx context.Context, id uint64, now time.Time) error {
	u, err := r.FindUser(ctx, id)
	if err != nil {
		return err
	}
	if u.Status == user.StatusVip {
		return nil
	}
	covering, err := r.hasCoveringGrant(ctx, id, now)
	if err != nil {
		return err
	}
	if covering {
		if u.Status == user.StatusPaid {
			return nil
		}
		return r.writeAccessStatus(ctx, u, user.StatusPaid)
	}
	if u.SubscriptionSource == "tebex" || u.SubscriptionSource == "admin" {
		return nil
	}
	if u.Status != user.StatusPaid {
		return nil
	}
	return r.writeAccessStatus(ctx, u, user.StatusFree)
}

func (r *Users) hasCoveringGrant(ctx context.Context, userID uint64, now time.Time) (bool, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (bool, error) {
		return r.client.PremiumGrant.Query().Where(
			premiumgrant.UserIDEQ(userID),
			premiumgrant.StateEQ(premiumgrant.StateCommitted),
			premiumgrant.StartAtLTE(now),
			premiumgrant.EndAtGT(now),
		).Exist(ctx)
	})
}

func (r *Users) writeAccessStatus(ctx context.Context, u *ent.User, status user.Status) error {
	err := db.WithExec(ctx, func(ctx context.Context) error {
		q := r.client.User.UpdateOneID(u.ID).Where(user.StatusNEQ(user.StatusVip)).SetStatus(status)
		if status == user.StatusPaid && u.SubscriptionSource == "" {
			q.SetSubscriptionSource("giveaway")
		}
		if status == user.StatusFree && u.SubscriptionSource == "giveaway" {
			q.SetSubscriptionSource("")
		}
		return q.Exec(ctx)
	})
	if err != nil {
		return err
	}
	r.views.Invalidate(cache.UserKey(userKeyPrefix, u.ID))
	r.stats.Invalidate(userStatsKey)
	return nil
}

// reconcileGrantAccess promotes already-announced covering grants whose user
// row is still free. Commit-time projection covers new awards; this catches
// winners whose grant was committed before status was stored.
func (r *Users) reconcileGrantAccess(ctx context.Context, now time.Time) error {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
		return r.client.User.Query().Where(
			user.StatusEQ(user.StatusFree),
			user.HasPremiumGrantsWith(
				premiumgrant.StateEQ(premiumgrant.StateCommitted),
				premiumgrant.StartAtLTE(now),
				premiumgrant.EndAtGT(now),
			),
		).Select(user.FieldID).All(ctx)
	})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := r.projectAccess(ctx, row.ID, now); err != nil {
			return err
		}
		if err := r.publishChanged(ctx, row.ID); err != nil {
			return err
		}
		if err := r.publishStatusInvalidation(ctx, row.ID); err != nil {
			return err
		}
	}
	return nil
}
