// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/tokens"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/objstore"

	"go.uber.org/zap"
)

const (
	erasurePrefix = "erasures/"
	// A record younger than this may belong to a Delete still committing.
	erasureSettle = time.Minute
	// Covers a deletion event lost after the database commit.
	erasureRepublishWindow = time.Hour
)

// Must stay outside the database: the ledger has to survive losing it.
type ErasureStore interface {
	PutNew(ctx context.Context, key string, body []byte) error
	List(ctx context.Context, prefix string) ([]objstore.Object, error)
}

type TokenRevoker interface {
	Revoke(ctx context.Context, accessToken string) error
}

type erasureRef struct {
	userID           uint64
	accountCreatedAt int64
}

func (r *Users) SetErasureStore(s ErasureStore) { r.erasures = s }

func (r *Users) SetTokenRevoker(t TokenRevoker) { r.revoker = t }

func erasureKey(ref erasureRef) string {
	return fmt.Sprintf("%s%d-%d", erasurePrefix, ref.userID, ref.accountCreatedAt)
}

func parseErasureKey(key string) (erasureRef, bool) {
	idPart, atPart, ok := strings.Cut(strings.TrimPrefix(key, erasurePrefix), "-")
	if !ok {
		return erasureRef{}, false
	}
	id, idErr := strconv.ParseUint(idPart, 10, 64)
	at, atErr := strconv.ParseInt(atPart, 10, 64)
	return erasureRef{userID: id, accountCreatedAt: at}, idErr == nil && atErr == nil
}

func (r *Users) recordErasure(ctx context.Context, ref erasureRef) error {
	if r.erasures == nil {
		return nil
	}
	err := r.erasures.PutNew(ctx, erasureKey(ref), nil)
	if errors.Is(err, objstore.ErrExists) {
		return nil
	}
	return err
}

func (r *Users) ReplayErasures(ctx context.Context, now time.Time) (int, error) {
	if r.erasures == nil {
		return 0, nil
	}
	pending, err := r.settledErasures(ctx, now)
	if err != nil || len(pending) == 0 {
		return 0, err
	}
	ids := make([]uint64, 0, len(pending))
	for ref := range pending {
		ids = append(ids, ref.userID)
	}
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
		return r.client.User.Query().Where(user.IDIn(ids...)).Select(user.FieldID, user.FieldCreatedAt).All(ctx)
	})
	if err != nil {
		return 0, err
	}
	erased, err := r.eraseRestored(ctx, rows, pending)
	if err != nil {
		return erased, err
	}
	return erased, r.republishRecent(ctx, rows, pending, now)
}

func (r *Users) eraseRestored(ctx context.Context, rows []*ent.User, pending map[erasureRef]time.Time) (int, error) {
	erased := 0
	for _, row := range rows {
		if _, ok := pending[erasureRef{userID: row.ID, accountCreatedAt: row.CreatedAt.UnixMicro()}]; !ok {
			continue
		}
		if err := r.erase(ctx, row.ID, row.CreatedAt); err != nil {
			return erased, err
		}
		erased++
	}
	return erased, nil
}

// Only when no account holds the id: a newer incarnation must never receive
// an old one's deletion event.
func (r *Users) republishRecent(ctx context.Context, rows []*ent.User, pending map[erasureRef]time.Time, now time.Time) error {
	present := make(map[uint64]bool, len(rows))
	for _, row := range rows {
		present[row.ID] = true
	}
	for ref, recordedAt := range pending {
		if present[ref.userID] || now.Sub(recordedAt) > erasureRepublishWindow {
			continue
		}
		dto := data.UserDeletedDTO{UserID: ref.userID, AccountCreatedAt: ref.accountCreatedAt, DeletedAt: recordedAt}
		if err := bus.PublishJSON(ctx, r.pub, data.SubjectUserDeleted, dto); err != nil {
			return err
		}
	}
	return nil
}

func (r *Users) settledErasures(ctx context.Context, now time.Time) (map[erasureRef]time.Time, error) {
	objects, err := r.erasures.List(ctx, erasurePrefix)
	if err != nil {
		return nil, err
	}
	pending := make(map[erasureRef]time.Time, len(objects))
	for _, obj := range objects {
		ref, ok := parseErasureKey(obj.Key)
		if ok && now.Sub(obj.LastModified) >= erasureSettle {
			pending[ref] = obj.LastModified
		}
	}
	return pending, nil
}

func (r *Users) revokeTwitchTokens(ctx context.Context, userID uint64) {
	if r.revoker == nil {
		return
	}
	for _, tokenType := range []tokens.Type{tokens.TypeAccessToken, tokens.TypeUserToken} {
		access, _, _, err := r.Token(ctx, userID, tokenType, tokens.PlatformTwitch)
		if err != nil {
			continue
		}
		if err := r.revoker.Revoke(ctx, string(access)); err != nil {
			r.log.Warn("twitch token revoke failed on account deletion", zap.Uint64("user_id", userID), zap.String("type", tokenType.String()), zap.Error(err))
		}
	}
}
