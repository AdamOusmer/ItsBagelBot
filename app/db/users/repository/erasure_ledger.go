// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/lifecycleerasure"
	"ItsBagelBot/app/db/users/ent/predicate"
	"ItsBagelBot/app/db/users/ent/tokens"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/objectstore"

	"go.uber.org/zap"
)

const (
	erasurePrefix = "erasures/"
	// A record younger than this may belong to a Delete still committing.
	erasureSettle     = time.Minute
	erasureQueryChunk = 256
)

// Must stay outside the database: the ledger has to survive losing it.
type ErasureStore interface {
	PutNew(ctx context.Context, key string, body []byte) error
	List(ctx context.Context, prefix string) ([]objectstore.Info, error)
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
	if errors.Is(err, objectstore.ErrExists) {
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
	refs := make([]erasureRef, 0, len(pending))
	for ref := range pending {
		refs = append(refs, ref)
	}
	slices.SortFunc(refs, compareErasureRefs)
	return r.replayErasureChunks(ctx, refs, pending)
}

func compareErasureRefs(a, b erasureRef) int {
	if order := cmp.Compare(a.userID, b.userID); order != 0 {
		return order
	}
	return cmp.Compare(a.accountCreatedAt, b.accountCreatedAt)
}

type erasureChunk struct {
	rows    []*ent.User
	pending map[erasureRef]time.Time
}

func (r *Users) replayErasureChunks(ctx context.Context, refs []erasureRef, pending map[erasureRef]time.Time) (int, error) {
	erased := 0
	var errs []error
	var chunks []erasureChunk
	for chunk := range slices.Chunk(refs, erasureQueryChunk) {
		records := make(map[erasureRef]time.Time, len(chunk))
		for _, ref := range chunk {
			records[ref] = pending[ref]
		}
		rows, err := r.erasureUsers(ctx, chunk)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		count, err := r.eraseRestored(ctx, rows, records)
		erased += count
		errs = append(errs, err)
		chunks = append(chunks, erasureChunk{rows: rows, pending: records})
	}
	for _, chunk := range chunks {
		errs = append(errs, r.republishAbsent(ctx, chunk.rows, chunk.pending))
	}
	return erased, errors.Join(errs...)
}

func (r *Users) erasureUsers(ctx context.Context, refs []erasureRef) ([]*ent.User, error) {
	ids := make([]uint64, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.userID)
	}
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
		return r.client.User.Query().Where(user.IDIn(ids...)).Order(ent.Asc(user.FieldID)).Select(user.FieldID, user.FieldCreatedAt).All(ctx)
	})
}

func (r *Users) eraseRestored(ctx context.Context, rows []*ent.User, pending map[erasureRef]time.Time) (int, error) {
	erased := 0
	var errs []error
	for _, row := range rows {
		if _, ok := pending[erasureRef{userID: row.ID, accountCreatedAt: row.CreatedAt.UnixMicro()}]; !ok {
			continue
		}
		if err := r.erase(ctx, row.ID, row.CreatedAt); err != nil {
			errs = append(errs, err)
			continue
		}
		erased++
	}
	return erased, errors.Join(errs...)
}

// A newer incarnation must never receive an old account's deletion event.
func (r *Users) republishAbsent(ctx context.Context, rows []*ent.User, pending map[erasureRef]time.Time) error {
	absent := absentErasures(rows, pending)
	replay, err := r.pendingDeletionTimes(ctx, absent)
	if err != nil {
		return err
	}
	deletions := make([]data.UserDeletedDTO, 0, len(replay))
	for _, row := range replay {
		deletions = append(deletions, data.UserDeletedDTO{UserID: row.UserID, AccountCreatedAt: row.AccountCreatedAt, DeletedAt: row.DeletedAt})
	}
	return r.publishDeletions(ctx, deletions)
}

func (r *Users) publishDeletions(ctx context.Context, deletions []data.UserDeletedDTO) error {
	var errs []error
	filters := make([]predicate.LifecycleErasure, 0, len(deletions))
	for _, dto := range deletions {
		if err := r.publishDeleted(ctx, dto); err != nil {
			errs = append(errs, err)
			continue
		}
		filters = append(filters, lifecycleerasure.And(lifecycleerasure.UserIDEQ(dto.UserID), lifecycleerasure.AccountCreatedAtEQ(dto.AccountCreatedAt)))
	}
	errs = append(errs, r.markDeletionsPublished(ctx, filters))
	return errors.Join(errs...)
}

func (r *Users) markDeletionsPublished(ctx context.Context, filters []predicate.LifecycleErasure) error {
	if len(filters) == 0 {
		return nil
	}
	err := db.RetryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			return r.client.LifecycleErasure.Update().Where(lifecycleerasure.Or(filters...), lifecycleerasure.DeletedPublishedAtIsNil()).SetDeletedPublishedAt(time.Now().UTC()).Exec(ctx)
		})
	})
	if err == nil || len(filters) == 1 {
		return err
	}
	mid := len(filters) / 2
	return errors.Join(r.markDeletionsPublished(ctx, filters[:mid]), r.markDeletionsPublished(ctx, filters[mid:]))
}

func absentErasures(rows []*ent.User, pending map[erasureRef]time.Time) map[erasureRef]time.Time {
	present := make(map[uint64]bool, len(rows))
	for _, row := range rows {
		present[row.ID] = true
	}
	absent := make(map[erasureRef]time.Time)
	for ref, recordedAt := range pending {
		if present[ref.userID] {
			continue
		}
		absent[ref] = recordedAt
	}
	return absent
}

// Consumers fence on deletion time, so prefer the committed time over the earlier ledger record.
func (r *Users) pendingDeletionTimes(ctx context.Context, pending map[erasureRef]time.Time) ([]*ent.LifecycleErasure, error) {
	if len(pending) == 0 {
		return nil, nil
	}
	filters := make([]predicate.LifecycleErasure, 0, len(pending))
	for ref := range pending {
		filters = append(filters, lifecycleerasure.And(lifecycleerasure.UserIDEQ(ref.userID), lifecycleerasure.AccountCreatedAtEQ(ref.accountCreatedAt)))
	}
	return db.WithRead(ctx, r.client.LifecycleErasure.Query().Where(
		lifecycleerasure.Or(filters...), lifecycleerasure.DeletedPublishedAtIsNil(),
	).Order(ent.Asc(lifecycleerasure.FieldUserID), ent.Asc(lifecycleerasure.FieldAccountCreatedAt)).All)
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
