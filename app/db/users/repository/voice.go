// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/ent/voiceusage"
	"ItsBagelBot/app/db/users/ent/voiceusagereceipt"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/db"
)

const (
	voiceReceiptRetention  = 40 * 24 * time.Hour
	maxVoiceRecordDuration = voiceplan.MaxClipSeconds * time.Second
)

var ErrInvalidVoiceUsage = errors.New("invalid voice usage")

type VoiceUsageRecord struct {
	RequestID   string
	AudioTokens int64
	DurationMS  int64
}

func (r *Users) VoiceAllowance(ctx context.Context, id uint64, now time.Time) (usersrpc.VoiceAllowance, error) {
	if err := validate.UserID(id); err != nil {
		return usersrpc.VoiceAllowance{}, err
	}
	return r.voiceTransaction(ctx, func(tx *ent.Tx) (usersrpc.VoiceAllowance, error) {
		owner, err := voiceOwnerQuery(tx, id).Only(ctx)
		if err != nil {
			return usersrpc.VoiceAllowance{}, err
		}
		return loadVoiceAllowance(ctx, tx, owner, now)
	})
}

func (r *Users) RecordVoiceUsage(ctx context.Context, id uint64, record VoiceUsageRecord, now time.Time) (usersrpc.VoiceAllowance, error) {
	if err := validate.UserID(id); err != nil {
		return usersrpc.VoiceAllowance{}, err
	}
	if !record.valid() {
		return usersrpc.VoiceAllowance{}, ErrInvalidVoiceUsage
	}
	return r.voiceTransaction(ctx, func(tx *ent.Tx) (usersrpc.VoiceAllowance, error) {
		owner, err := voiceOwnerQuery(tx, id).Where(lockOwnerRow).Only(ctx)
		if err != nil {
			return usersrpc.VoiceAllowance{}, err
		}
		return voiceRecording{record: record, now: now.UTC(), owner: owner}.apply(ctx, tx)
	})
}

func voiceOwnerQuery(tx *ent.Tx, id uint64) *ent.UserSelect {
	return tx.User.Query().Where(user.IDEQ(id)).Select(
		user.FieldID, user.FieldStatus, user.FieldSubscriptionSource, user.FieldIsActive, user.FieldBanned,
	)
}

func (r *Users) voiceTransaction(ctx context.Context, run func(*ent.Tx) (usersrpc.VoiceAllowance, error)) (usersrpc.VoiceAllowance, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (usersrpc.VoiceAllowance, error) {
		return db.RetryWithTxResult(ctx, r.client, run)
	})
}

func (r *Users) CleanupVoiceUsageReceipts(ctx context.Context, now time.Time) (int, error) {
	cutoff := now.UTC().Add(-voiceReceiptRetention)
	return db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.VoiceUsageReceipt.Delete().Where(voiceusagereceipt.CreatedAtLTE(cutoff)).Exec(ctx)
	})
}

func (r VoiceUsageRecord) valid() bool {
	length := len(r.RequestID)
	if length == 0 || length > usersrpc.VoiceRequestIDMaxBytes {
		return false
	}
	validTokens := r.AudioTokens >= 0 && r.AudioTokens <= voiceplan.SecondsToTokens(voiceplan.MaxClipSeconds)
	validDuration := r.DurationMS >= 0 && r.DurationMS <= maxVoiceRecordDuration.Milliseconds()
	return validTokens && validDuration
}

type voiceRecording struct {
	record VoiceUsageRecord
	now    time.Time
	owner  *ent.User
}

func (v voiceRecording) apply(ctx context.Context, tx *ent.Tx) (usersrpc.VoiceAllowance, error) {
	receipt, err := tx.VoiceUsageReceipt.Query().Where(
		voiceusagereceipt.UserIDEQ(v.owner.ID),
		voiceusagereceipt.RequestIDEQ(v.record.RequestID),
	).Only(ctx)
	if err == nil {
		return receipt.Allowance, nil
	}
	if !ent.IsNotFound(err) {
		return usersrpc.VoiceAllowance{}, err
	}
	if err := v.addUsage(ctx, tx); err != nil {
		return usersrpc.VoiceAllowance{}, err
	}
	allowance, err := loadVoiceAllowance(ctx, tx, v.owner, v.now)
	if err != nil {
		return usersrpc.VoiceAllowance{}, err
	}
	err = tx.VoiceUsageReceipt.Create().
		SetUserID(v.owner.ID).SetRequestID(v.record.RequestID).SetPeriod(allowance.Period).
		SetAudioTokens(v.record.AudioTokens).SetCreatedAt(v.now).SetAllowance(allowance).Exec(ctx)
	return allowance, err
}

func (v voiceRecording) addUsage(ctx context.Context, tx *ent.Tx) error {
	return tx.VoiceUsage.Create().
		SetUserID(v.owner.ID).SetPeriod(voiceplan.Period(v.now)).
		SetAudioTokens(v.record.AudioTokens).SetDurationMs(v.record.DurationMS).SetRequests(1).SetUpdatedAt(v.now).
		OnConflictColumns(voiceusage.FieldUserID, voiceusage.FieldPeriod).
		Update(func(upsert *ent.VoiceUsageUpsert) {
			upsert.AddAudioTokens(v.record.AudioTokens).
				AddDurationMs(v.record.DurationMS).AddRequests(1).SetUpdatedAt(v.now)
		}).Exec(ctx)
}

func loadVoiceAllowance(ctx context.Context, tx *ent.Tx, owner *ent.User, now time.Time) (usersrpc.VoiceAllowance, error) {
	period := voiceplan.Period(now)
	usage, err := tx.VoiceUsage.Query().Where(voiceusage.UserIDEQ(owner.ID), voiceusage.PeriodEQ(period)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return usersrpc.VoiceAllowance{}, err
	}
	var used int64
	if usage != nil {
		used = usage.AudioTokens
	}
	plan, err := loadVoicePlan(ctx, tx, owner, now)
	if err != nil {
		return usersrpc.VoiceAllowance{}, err
	}
	limit := voiceplan.SecondsToTokens(voiceplan.MonthlySeconds(plan))
	allowance := usersrpc.VoiceAllowance{
		Plan: plan, Period: period, LimitTokens: limit, UsedTokens: used, RemainingTokens: max(0, limit-used),
		ResetsAt: voiceplan.ResetsAt(now),
		Reason:   voiceAllowanceReason(owner, plan),
	}
	return allowance, nil
}

func voiceAllowanceReason(owner *ent.User, plan voiceplan.Plan) string {
	switch {
	case owner.Banned:
		return "banned"
	case !owner.IsActive:
		return "inactive"
	case plan == voiceplan.Free:
		return "no_plan"
	default:
		return "ok"
	}
}

func loadVoicePlan(ctx context.Context, tx *ent.Tx, owner *ent.User, now time.Time) (voiceplan.Plan, error) {
	plan := voiceplan.PlanFor(string(owner.Status), owner.SubscriptionSource, owner.IsActive, owner.Banned)
	if plan != voiceplan.AdminGrant {
		return plan, nil
	}
	covering, err := hasCoveringGrant(ctx, tx.PremiumGrant, owner.ID, now.UTC())
	if !covering || err != nil {
		return plan, err
	}
	return voiceplan.Giveaway, nil
}
