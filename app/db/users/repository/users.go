// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/predicate"
	"ItsBagelBot/app/db/users/ent/tokens"
	"ItsBagelBot/app/db/users/ent/user"
	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/batch"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const (
	userKeyPrefix = "user:"

	userCacheTTL = 5 * time.Minute

	userCacheCapacity int64 = 4096
)

// UserView is cached in process, so it must carry no sensitive fields.
type UserView struct {
	StateRevision             int64      `json:"state_revision,omitempty"`
	ID                        uint64     `json:"id"`
	AccountCreatedAt          int64      `json:"account_created_at,omitempty"`
	Username                  string     `json:"username"`
	DisplayName               string     `json:"display_name"`
	IsActive                  bool       `json:"is_active"`
	Status                    string     `json:"status"`
	Banned                    bool       `json:"banned"`
	Locale                    string     `json:"locale"`
	CustomCursor              bool       `json:"custom_cursor"`
	CommandsPageHidden        bool       `json:"commands_page_hidden"`
	CreatorCode               *string    `json:"creator_code,omitempty"`
	SubscriptionSource        string     `json:"subscription_source"`
	SubscriptionExpiresAt     *time.Time `json:"subscription_expires_at,omitempty"`
	SubscriptionRef           *string    `json:"subscription_ref,omitempty"`
	SubscriptionCancelPending bool       `json:"subscription_cancel_pending"`
	Onboarded                 bool       `json:"onboarded"`
}

type Users struct {
	client             *ent.Client
	views              *cache.Cache[UserView]
	stats              *cache.Cache[userStatsRow]
	packer             domaincrypto.Packer
	pub                bus.Publisher
	batcher            *batch.Batcher[prefKey, prefWrite]
	pendingMu          sync.RWMutex
	pendingPrefs       map[prefKey]prefWrite
	app                *newrelic.Application
	log                *zap.Logger
	invalidationPrefix string
	nc                 *nats.Conn
}

func (r *Users) SetInvalidationPrefix(prefix string) {
	r.invalidationPrefix = strings.TrimSuffix(strings.TrimSpace(prefix), ".")
}

func (r *Users) SetInvalidationConn(nc *nats.Conn) {
	r.nc = nc
}

func NewUsers(client *ent.Client, packer domaincrypto.Packer, pub bus.Publisher, app *newrelic.Application, log *zap.Logger) *Users {

	if log == nil {
		log = zap.NewNop()
	}

	r := &Users{
		client:       client,
		views:        cache.New[UserView](userCacheCapacity, userCacheTTL),
		stats:        cache.New[userStatsRow](userStatsCapacity, userStatsTTL),
		packer:       packer,
		pub:          pub,
		app:          app,
		log:          log,
		pendingPrefs: make(map[prefKey]prefWrite),
	}

	r.batcher = batch.New[prefKey, prefWrite](prefsFlushInterval, prefsFlushMaxSize, r.flushPrefs, log)

	return r
}

type displayName string

func (d displayName) storable() displayName {
	if len(d) > 64 {
		return ""
	}
	return d
}

func (d displayName) changes(existing *ent.User, username string) bool {
	return existing.Username != username || (d != "" && existing.DisplayName != string(d))
}

func (d displayName) apply(upd *ent.UserUpdateOne) *ent.UserUpdateOne {
	if d == "" {
		return upd
	}
	return upd.SetDisplayName(string(d))
}

func (r *Users) Register(ctx context.Context, id uint64, username, rawDisplayName, email string) error {
	if err := validate.UserID(id); err != nil {
		return err
	}
	if err := validate.Username(username); err != nil {
		return err
	}
	if err := validate.Email(email); err != nil {
		return err
	}
	name := displayName(rawDisplayName).storable()

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		existing, err := r.client.User.Query().
			Where(user.IDEQ(id)).
			Only(ctx)

		switch {
		case ent.IsNotFound(err):
			_, err = r.client.User.Create().
				SetID(id).
				SetUsername(username).
				SetDisplayName(string(name)).
				SetEmail(email).
				Save(ctx)
			if ent.IsConstraintError(err) {
				_, err = name.apply(r.client.User.UpdateOneID(id).SetUsername(username)).
					Save(ctx)
			}

		case err == nil && name.changes(existing, username):
			_, err = name.apply(existing.Update().SetUsername(username)).
				Save(ctx)
		}

		return err
	}); err != nil {
		return err
	}

	return r.publishChanged(ctx, id)
}

func (r *Users) Get(ctx context.Context, id uint64) (UserView, error) {

	return r.views.GetOrLoad(ctx, cache.UserKey(userKeyPrefix, id), func(ctx context.Context) (UserView, error) {
		return db.WithQuery(ctx, func(ctx context.Context) (UserView, error) {

			u, err := r.client.User.Query().
				Where(user.IDEQ(id)).
				Only(ctx)
			if err != nil {
				return UserView{}, err
			}

			return UserView{
				ID:                        u.ID,
				AccountCreatedAt:          u.CreatedAt.UnixMicro(),
				StateRevision:             u.StateRevision,
				Username:                  u.Username,
				DisplayName:               u.DisplayName,
				IsActive:                  u.IsActive,
				Status:                    string(u.Status),
				Banned:                    u.Banned,
				Locale:                    u.Locale,
				CustomCursor:              u.CustomCursor,
				CommandsPageHidden:        u.CommandsPageHidden,
				CreatorCode:               u.CreatorCode,
				SubscriptionSource:        u.SubscriptionSource,
				SubscriptionExpiresAt:     u.SubscriptionExpiresAt,
				SubscriptionRef:           u.SubscriptionRef,
				SubscriptionCancelPending: u.SubscriptionCancelPending,
				Onboarded:                 u.Onboarded,
			}, nil
		})
	})
}

// Must order by updated_at and stay uncached: a renamed login can still sit on our old row.
func (r *Users) IDByUsername(ctx context.Context, username string) (uint64, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if err := validate.Username(username); err != nil {
		return 0, err
	}

	return db.WithQuery(ctx, func(ctx context.Context) (uint64, error) {
		row, err := r.client.User.Query().
			Where(user.UsernameEQ(username)).
			Order(ent.Desc(user.FieldUpdatedAt)).
			Select(user.FieldID).
			First(ctx)
		if err != nil {
			return 0, err
		}
		return row.ID, nil
	})
}

func (r *Users) updateAndPublish(ctx context.Context, id uint64, apply func(*ent.UserUpdateOne)) error {
	if err := validate.UserID(id); err != nil {
		return err
	}
	if err := db.WithExec(ctx, func(ctx context.Context) error {
		update := r.client.User.UpdateOneID(id)
		apply(update)
		return update.Exec(ctx)
	}); err != nil {
		return err
	}
	return r.publishChanged(ctx, id)
}

const CreatorCodeMaxLen = 64

func normalizeCreatorCode(raw string) (*string, error) {
	code := strings.TrimSpace(raw)
	if code == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(code) > CreatorCodeMaxLen {
		return nil, fmt.Errorf("creator_code must be %d characters or fewer", CreatorCodeMaxLen)
	}
	for _, r := range code {
		if r < 0x20 || r == 0x7f {
			return nil, fmt.Errorf("creator_code cannot contain control characters")
		}
	}
	return &code, nil
}

func (r *Users) SetCreatorCode(ctx context.Context, id uint64, raw string) error {
	code, err := normalizeCreatorCode(raw)
	if err != nil {
		return err
	}
	r.queuePref(id, prefCreatorCode, prefWrite{code: code})
	return nil
}

// SetStatus is on the money path, so it must write through, never via the batcher.
func (r *Users) SetStatus(ctx context.Context, id uint64, status user.Status) error {
	if err := validate.Status(string(status)); err != nil {
		return err
	}
	return r.updateAndPublish(ctx, id, func(u *ent.UserUpdateOne) { u.SetStatus(status) })
}

func (r *Users) SetActive(ctx context.Context, id uint64, active bool) error {
	r.queuePref(id, prefActive, prefWrite{flag: active})
	return nil
}

func (r *Users) SetLocale(ctx context.Context, id uint64, locale string) error {
	r.queuePref(id, prefLocale, prefWrite{str: locale})
	return nil
}

func (r *Users) SetCustomCursor(ctx context.Context, id uint64, on bool) error {
	r.queuePref(id, prefCursor, prefWrite{flag: on})
	return nil
}

// SetBanned is moderation enforcement, so it writes through.
func (r *Users) SetBanned(ctx context.Context, id uint64, banned bool) error {
	return r.updateAndPublish(ctx, id, func(u *ent.UserUpdateOne) { u.SetBanned(banned) })
}

// Must write through: Get does not overlay pending preference writes.
func (r *Users) SetCommandsPageHidden(ctx context.Context, id uint64, hidden bool) error {
	return r.updateAndPublish(ctx, id, func(u *ent.UserUpdateOne) { u.SetCommandsPageHidden(hidden) })
}

func (r *Users) SetOnboarded(ctx context.Context, id uint64, onboarded bool) error {
	r.queuePref(id, prefOnboarded, prefWrite{flag: onboarded})
	return nil
}

func (r *Users) Delete(ctx context.Context, id uint64) error {
	var accountCreatedAt int64

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		row, err := r.client.User.Query().Where(user.IDEQ(id)).Only(ctx)
		if err != nil {
			return err
		}
		accountCreatedAt = row.CreatedAt.UnixMicro()
		// Match the incarnation read above so a concurrent delete/register
		// cannot turn this request into deletion of the replacement account.
		n, err := r.client.User.Delete().Where(user.IDEQ(id), user.CreatedAtEQ(row.CreatedAt)).Exec(ctx)
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("user account changed during deletion")
		}
		return nil
	}); err != nil {
		return err
	}

	r.views.Invalidate(cache.UserKey(userKeyPrefix, id))

	return bus.PublishJSON(ctx, r.pub, data.SubjectUserDeleted, data.UserDeletedDTO{UserID: id, AccountCreatedAt: accountCreatedAt})
}

func (r *Users) Invalidate(id uint64) {
	r.views.Invalidate(cache.UserKey(userKeyPrefix, id))
}

func (r *Users) Close(ctx context.Context) {
	r.batcher.Close(ctx)
	r.views.Close()
	r.stats.Close()
}

func (r *Users) publishChanged(ctx context.Context, id uint64) error {

	r.views.Invalidate(cache.UserKey(userKeyPrefix, id))

	view, err := r.Get(ctx, id)
	if err != nil {
		return err
	}

	return bus.PublishJSON(ctx, r.pub, data.SubjectUserChanged, data.UserChangedDTO{
		UserID:             view.ID,
		AccountCreatedAt:   view.AccountCreatedAt,
		StateRevision:      view.StateRevision,
		Username:           view.Username,
		IsActive:           view.IsActive,
		Status:             view.Status,
		Banned:             view.Banned,
		Locale:             view.Locale,
		CommandsPageHidden: view.CommandsPageHidden,
	})
}

func (r *Users) publishStatusInvalidation(_ context.Context, id uint64) error {
	// Must be core NATS: bagel.cache.invalidate.* has no JetStream stream.
	if r.nc == nil || r.invalidationPrefix == "" {
		return nil
	}
	return invalidate.Publish(r.nc, r.invalidationPrefix, "status", strconv.FormatUint(id, 10))
}

func (r *Users) Reproject(ctx context.Context) error {

	const pageSize = 500

	var afterID uint64

	for {
		rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
			return r.client.User.Query().
				Where(user.IDGT(afterID)).
				Order(ent.Asc(user.FieldID)).
				Limit(pageSize).
				All(ctx)
		})
		if err != nil {
			return err
		}

		for _, row := range rows {
			if err := r.publishReprojectedUser(ctx, row); err != nil {
				return err
			}
		}

		if len(rows) < pageSize {
			return nil
		}

		afterID = rows[len(rows)-1].ID
	}
}

func (r *Users) publishReprojectedUser(ctx context.Context, row *ent.User) error {
	return bus.PublishJSON(ctx, r.pub, data.SubjectUserChanged, data.UserChangedDTO{
		UserID: row.ID, Username: row.Username, IsActive: row.IsActive,
		AccountCreatedAt: row.CreatedAt.UnixMicro(),
		StateRevision:    row.StateRevision,
		Status:           string(row.Status), Banned: row.Banned, Locale: row.Locale,
	})
}

func (r *Users) UpsertToken(ctx context.Context, userID uint64, tokenType tokens.Type, platform tokens.Platform, accessToken []byte, refreshToken []byte, accessTokenExpiresAt *time.Time) error {

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.Token(accessToken); err != nil {
		return err
	}
	if len(refreshToken) > 0 {
		if err := validate.Token(refreshToken); err != nil {
			return err
		}
	}

	aad := tokenOwner(userID).aad(tokenType, platform)

	sealed, err := r.packer.Pack(accessToken, aad)
	if err != nil {
		return err
	}

	var sealedRefresh domaincrypto.SecureEnvelope
	if len(refreshToken) > 0 {
		if sealedRefresh, err = r.packer.Pack(refreshToken, aad); err != nil {
			return err
		}
	}

	return db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, r.client, func(tx *ent.Tx) error {

			existing, err := tx.Tokens.Query().
				Where(
					tokens.TypeEQ(tokenType),
					tokens.PlatformEQ(platform),
					tokenOwner(userID).match(),
				).
				Only(ctx)

			switch {
			case ent.IsNotFound(err):
				create := tx.Tokens.Create().
					SetUserID(userID).
					SetType(tokenType).
					SetPlatform(platform).
					SetToken(sealed.Ciphertext).
					SetNillableAccessTokenExpiresAt(accessTokenExpiresAt)

				if len(sealedRefresh.Ciphertext) > 0 {
					create.SetRefreshToken(sealedRefresh.Ciphertext)
				}

				if err := create.Exec(ctx); err != nil {
					if ent.IsConstraintError(err) {
						existing, err = tx.Tokens.Query().
							Where(
								tokens.TypeEQ(tokenType),
								tokens.PlatformEQ(platform),
								tokenOwner(userID).match(),
							).
							Only(ctx)
						if err != nil {
							return err
						}
						update := applyTokenExpiry(existing.Update().SetToken(sealed.Ciphertext), accessTokenExpiresAt)
						if len(sealedRefresh.Ciphertext) > 0 {
							update.SetRefreshToken(sealedRefresh.Ciphertext)
						}
						return update.Exec(ctx)
					}
					return err
				}
				return nil

			case err != nil:
				return err
			}

			update := applyTokenExpiry(existing.Update().SetToken(sealed.Ciphertext), accessTokenExpiresAt)

			if len(sealedRefresh.Ciphertext) > 0 {
				update.SetRefreshToken(sealedRefresh.Ciphertext)
			}

			return update.Exec(ctx)
		})
	})
}

type tokenOwner uint64

func (o tokenOwner) match() predicate.Tokens {
	return predicate.Tokens(func(s *entsql.Selector) {
		s.Where(entsql.EQ(s.C(tokens.UserColumn), uint64(o)))
	})
}

func (r *Users) Token(ctx context.Context, userID uint64, tokenType tokens.Type, platform tokens.Platform) (accessToken []byte, refreshToken []byte, accessTokenExpiresAt *time.Time, err error) {

	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.Tokens, error) {
		return r.client.Tokens.Query().
			Where(
				tokens.TypeEQ(tokenType),
				tokens.PlatformEQ(platform),
				tokenOwner(userID).match(),
			).
			Only(ctx)
	})
	if err != nil {
		return nil, nil, nil, err
	}

	aad := tokenOwner(userID).aad(tokenType, platform)

	accessToken, err = r.packer.Unpack(domaincrypto.SecureEnvelope{Ciphertext: row.Token, AttachedData: aad})
	if err != nil {
		return nil, nil, nil, err
	}

	if len(row.RefreshToken) > 0 {
		if refreshToken, err = r.packer.Unpack(domaincrypto.SecureEnvelope{Ciphertext: row.RefreshToken, AttachedData: aad}); err != nil {
			return nil, nil, nil, err
		}
	}

	return accessToken, refreshToken, row.AccessTokenExpiresAt, nil
}

// A nil expiry must clear: the row may still carry the replaced token's expiry.
func applyTokenExpiry(u *ent.TokensUpdateOne, expiresAt *time.Time) *ent.TokensUpdateOne {
	if expiresAt != nil {
		return u.SetAccessTokenExpiresAt(*expiresAt)
	}
	return u.ClearAccessTokenExpiresAt()
}

func (o tokenOwner) aad(tokenType tokens.Type, platform tokens.Platform) []byte {

	aad := make([]byte, 0, 20+1+len(tokenType)+1+len(platform))

	aad = strconv.AppendUint(aad, uint64(o), 10)
	aad = append(aad, '|')
	aad = append(aad, tokenType...)
	aad = append(aad, '|')
	aad = append(aad, platform...)

	return aad
}

func withTx(ctx context.Context, client *ent.Client, fn func(tx *ent.Tx) error) error {

	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}
