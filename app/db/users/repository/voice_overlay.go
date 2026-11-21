// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"

	"go.uber.org/zap"
)

var (
	ErrOverlayTokenNotFound = errors.New("overlay token not found")
	ErrOverlayUnavailable   = errors.New("voice overlay unavailable for this account")
)

func (r *Users) RotateOverlayToken(ctx context.Context, userID uint64) (string, error) {
	var seed [32]byte
	rand.Read(seed[:])
	token := base64.RawURLEncoding.EncodeToString(seed[:])
	sum := sha256.Sum256([]byte(token))
	if err := r.saveOverlayToken(ctx, userID, overlayTokenHash(hex.EncodeToString(sum[:]))); err != nil {
		return "", err
	}
	r.publishOverlayRevoked(ctx, userID)
	return token, nil
}

func (r *Users) saveOverlayToken(ctx context.Context, userID uint64, hash overlayTokenHash) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		return db.RetryWithTx(ctx, r.client, func(tx *ent.Tx) error {
			return rotateOverlayToken(ctx, tx, userID, hash)
		})
	})
}

type overlayTokenHash string

func rotateOverlayToken(ctx context.Context, tx *ent.Tx, userID uint64, hash overlayTokenHash) error {
	owner, err := tx.User.Query().Where(user.IDEQ(userID), lockOwnerRow).Only(ctx)
	if err != nil {
		return err
	}
	plan := voiceplan.PlanFor(string(owner.Status), owner.SubscriptionSource, owner.IsActive, owner.Banned)
	if voiceplan.MonthlySeconds(plan) == 0 {
		return ErrOverlayUnavailable
	}
	// Keep old cookies invalid even when the same Twitch ID registers again after erasure.
	generation := max(owner.TtsOverlayGeneration+1, int(time.Now().UnixMicro()))
	return tx.User.UpdateOne(owner).
		SetTtsOverlayTokenHash(string(hash)).SetTtsOverlayGeneration(generation).Exec(ctx)
}

func (r *Users) ResolveOverlayToken(ctx context.Context, hash string) (uint64, int, error) {
	if !overlayTokenHash(hash).valid() {
		return 0, 0, ErrOverlayTokenNotFound
	}
	owner, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.User, error) {
		return r.client.User.Query().
			Where(user.TtsOverlayTokenHashEQ(hash), user.IsActiveEQ(true), user.BannedEQ(false)).
			Select(user.FieldID, user.FieldTtsOverlayGeneration).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return 0, 0, ErrOverlayTokenNotFound
	}
	if err != nil {
		return 0, 0, err
	}
	return owner.ID, owner.TtsOverlayGeneration, nil
}

func (h overlayTokenHash) valid() bool {
	hash := string(h)
	if len(hash) != sha256.Size*2 || hash != strings.ToLower(hash) {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}

func (r *Users) OverlayGenerationValid(ctx context.Context, userID uint64, generation int) bool {
	valid, err := db.WithQuery(ctx, func(ctx context.Context) (bool, error) {
		return r.client.User.Query().
			Where(user.IDEQ(userID), user.TtsOverlayGenerationEQ(generation), user.TtsOverlayTokenHashNotNil(),
				user.IsActiveEQ(true), user.BannedEQ(false)).Exist(ctx)
	})
	if err != nil {
		r.log.Warn("voice overlay check failed", zap.Uint64("user_id", userID), zap.Error(err))
	}
	return err == nil && valid
}

func clearOverlayToken(update *ent.UserUpdateOne) *ent.UserUpdateOne {
	return update.ClearTtsOverlayTokenHash().AddTtsOverlayGeneration(1)
}

func (r *Users) publishOverlayRevoked(ctx context.Context, userID uint64) {
	if r.nc == nil {
		return
	}
	subject := usersrpc.VoiceOverlayRevokedPrefix + strconv.FormatUint(userID, 10)
	if err := bus.PublishCore(ctx, r.nc, subject, nil); err != nil {
		r.log.Warn("voice overlay revocation publish failed", zap.Uint64("user_id", userID), zap.Error(err))
	}
}
