// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/voiceusage"
	"ItsBagelBot/internal/adminauth"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

var (
	ErrInvalidVoiceUsageChange = errors.New("choose reset or a whole usage percentage from 0 to 100")
	ErrNoVoiceAllowance        = errors.New("user has no Premium voice allowance")
	ErrVoiceUsageForbidden     = errors.New("voice usage adjustment requires active admin staff")
)

type VoiceUsageChange struct {
	Reset       bool
	UsedPercent *int
}

type AdminVoiceUsageChange struct {
	VoiceUsageChange
	ActorID string
}

type voiceAdjustment struct {
	VoiceUsageChange
	userID uint64
	now    time.Time
	audit  usersrpc.AuthRequest
}

type voiceAdjustmentResult struct {
	allowance usersrpc.VoiceAllowance
	delivery  *ent.AdminAuditDelivery
}

func (r *Users) AdjustVoiceUsage(ctx context.Context, userID uint64, change VoiceUsageChange, now time.Time) (usersrpc.VoiceAllowance, error) {
	return r.adjustVoiceUsage(ctx, voiceAdjustment{VoiceUsageChange: change, userID: userID, now: now.UTC()})
}

func (r *Users) AdjustAdminVoiceUsage(ctx context.Context, userID uint64, change AdminVoiceUsageChange, now time.Time) (usersrpc.VoiceAllowance, error) {
	actor, err := r.adminAuth.Authorize(ctx, change.ActorID, adminauth.Admin)
	if err != nil {
		return usersrpc.VoiceAllowance{}, fmt.Errorf("%w: %w", ErrVoiceUsageForbidden, err)
	}
	return r.adjustVoiceUsage(ctx, voiceAdjustment{
		VoiceUsageChange: change.VoiceUsageChange, userID: userID, now: now.UTC(),
		audit: usersrpc.AuthRequest{
			ActorID: change.ActorID, ActorLogin: actor.Login, Action: usersrpc.AdminVoiceUsageAuditAction,
			Target: fmt.Sprint(userID), OK: true,
		},
	})
}

func (r *Users) adjustVoiceUsage(ctx context.Context, adjustment voiceAdjustment) (usersrpc.VoiceAllowance, error) {
	if err := validate.UserID(adjustment.userID); err != nil {
		return usersrpc.VoiceAllowance{}, err
	}
	if !adjustment.valid() {
		return usersrpc.VoiceAllowance{}, ErrInvalidVoiceUsageChange
	}
	result, err := db.WithQuery(ctx, func(ctx context.Context) (voiceAdjustmentResult, error) {
		return db.RetryWithTxResult(ctx, r.client, func(tx *ent.Tx) (voiceAdjustmentResult, error) {
			return adjustment.apply(ctx, tx)
		})
	})
	if err != nil {
		return usersrpc.VoiceAllowance{}, err
	}
	if result.delivery != nil {
		r.tryDeliverAdminAudit(ctx, result.delivery)
	}
	return result.allowance, nil
}

func (change VoiceUsageChange) valid() bool {
	if change.Reset {
		return change.UsedPercent == nil
	}
	return change.UsedPercent != nil && *change.UsedPercent >= 0 && *change.UsedPercent <= 100
}

func (a voiceAdjustment) apply(ctx context.Context, tx *ent.Tx) (voiceAdjustmentResult, error) {
	owner, err := voiceOwnerQuery(tx, a.userID).Where(lockOwnerRow).Only(ctx)
	if err != nil {
		return voiceAdjustmentResult{}, err
	}
	allowance, err := loadVoiceAllowance(ctx, tx, owner, a.now)
	if err != nil {
		return voiceAdjustmentResult{}, err
	}
	if allowance.LimitTokens == 0 {
		return voiceAdjustmentResult{}, ErrNoVoiceAllowance
	}
	oldUsed := allowance.UsedTokens
	allowance.UsedTokens = a.usedTokens(allowance.LimitTokens)
	allowance.RemainingTokens = allowance.LimitTokens - allowance.UsedTokens
	if err := a.save(ctx, tx, allowance); err != nil {
		return voiceAdjustmentResult{}, err
	}
	delivery, err := a.queueAudit(ctx, tx, allowance, oldUsed)
	return voiceAdjustmentResult{allowance: allowance, delivery: delivery}, err
}

func (a voiceAdjustment) usedTokens(limit int64) int64 {
	if a.Reset {
		return 0
	}
	return limit * int64(*a.UsedPercent) / 100
}

func (a voiceAdjustment) save(ctx context.Context, tx *ent.Tx, allowance usersrpc.VoiceAllowance) error {
	return tx.VoiceUsage.Create().
		SetUserID(a.userID).SetPeriod(allowance.Period).SetAudioTokens(allowance.UsedTokens).
		SetDurationMs(0).SetRequests(0).SetUpdatedAt(a.now).
		OnConflictColumns(voiceusage.FieldUserID, voiceusage.FieldPeriod).
		Update(func(upsert *ent.VoiceUsageUpsert) {
			upsert.SetAudioTokens(allowance.UsedTokens).SetUpdatedAt(a.now)
		}).Exec(ctx)
}

func (a voiceAdjustment) queueAudit(ctx context.Context, tx *ent.Tx, allowance usersrpc.VoiceAllowance, oldUsed int64) (*ent.AdminAuditDelivery, error) {
	if a.audit.Action == "" {
		return nil, nil
	}
	operation := "set"
	if a.Reset {
		operation = "reset"
	}
	a.audit.Detail = fmt.Sprintf("operation=%s old_used_tokens=%d new_used_tokens=%d period=%s",
		operation, oldUsed, allowance.UsedTokens, allowance.Period)
	return tx.AdminAuditDelivery.Create().SetPayload(a.audit).Save(ctx)
}
