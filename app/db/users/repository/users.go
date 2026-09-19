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

	// userCacheCapacity ceilings the view cache. It is keyed one entry per user,
	// so a few thousand covers the users read within the 5m TTL without holding
	// the generic cache.DefaultCapacity ten thousand resident at rest.
	userCacheCapacity int64 = 4096
)

// UserView is the read model served from the in-process cache. It carries no
// sensitive fields, so holding it in memory is safe.
type UserView struct {
	ID                        uint64     `json:"id"`
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

// Users persists the user records and their OAuth tokens. Reads are served
// from the in-process cache with stampede protection. Writes split by the
// ADR-0008 durability classes: preference state (active, locale, cursor,
// onboarded, creator code) goes through the write-behind batcher — a user
// flipping the same switch five times costs one row write, and a burst lands
// as one transaction, so setters report "accepted", not "persisted"; errors
// surface at flush and are requeued or dropped there. Money (status tier),
// moderation (banned) and tokens write through immediately.
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

// displayName is Twitch's cased form of a login as it arrived on a login
// event; empty when the event carried none.
type displayName string

// storable is the display name as the row can hold it. Twitch display names
// are the login in the owner's casing, at most 25 characters, but localized
// names can run to three bytes a character; anything past the column's 64
// bytes drops to "" (readers fall back to the login) rather than failing the
// login it arrived with.
func (d displayName) storable() displayName {
	if len(d) > 64 {
		return ""
	}
	return d
}

// changes reports whether a login should rewrite the row's names. An empty
// display name never counts: a caller that has none must not blank a stored
// one.
func (d displayName) changes(existing *ent.User, username string) bool {
	return existing.Username != username || (d != "" && existing.DisplayName != string(d))
}

// apply sets the display name on an update only when there is one.
func (d displayName) apply(upd *ent.UserUpdateOne) *ent.UserUpdateOne {
	if d == "" {
		return upd
	}
	return upd.SetDisplayName(string(d))
}

// Register creates the user row on first login and refreshes the names it
// carries on every later one. displayName is Twitch's cased form of the login
// and may be empty.
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

// Get returns the cached view of the user; concurrent misses on the same ID
// collapse into a single query.
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

// IDByUsername resolves a Twitch login to its broadcaster id. It backs the
// public command page, whose URL is keyed by login (/user/<login>) so the link
// a viewer sees names the channel it serves; the page then reads everything
// else from the id this returns. The id, never the URL, decides whose commands
// render -- the page used to take the channel label from a query string, which
// let anyone rewrite a shared link to attribute one channel's commands to
// another handle.
//
// Ordering by updated_at is not cosmetic: username carries no unique
// constraint because a Twitch rename frees the old login for someone else, and
// our row keeps the stale login until that user next signs in. When two rows
// collide the freshest write is the one Twitch agrees with, so the lookup
// takes it instead of failing the page. Matching is left to the column's
// collation (utf8mb4 _ci), which is case-insensitive; the caller lowercases
// anyway so the cache key is stable.
//
// Deliberately uncached here: the console caches the resolve under its own
// login key with a fabric policy, and a second in-process cache keyed by name
// would keep serving a login after a rename moved it to a different id.
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

// updateAndPublish validates the id, applies a single-field update inside the
// write-through exec, and announces the change so the projector folds it into
// the Valkey user projection. It backs only the mutators that must never sit
// in a write-behind buffer: SetStatus (money) and SetBanned (moderation).
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

// SetCreatorCode stores or clears the user's public creator code. An empty
// value clears the nullable column. Validation runs synchronously so bad
// input still errors at the call site; persistence is write-behind (accepted,
// not persisted) and the change event rides the flush after the commit.
func (r *Users) SetCreatorCode(ctx context.Context, id uint64, raw string) error {
	code, err := normalizeCreatorCode(raw)
	if err != nil {
		return err
	}
	r.queuePref(id, prefCreatorCode, prefWrite{code: code})
	return nil
}

// SetStatus moves the user between the free, paid and vip tiers. This is on
// the money path, so it writes through immediately, never via the batcher.
func (r *Users) SetStatus(ctx context.Context, id uint64, status user.Status) error {
	if err := validate.Status(string(status)); err != nil {
		return err
	}
	return r.updateAndPublish(ctx, id, func(u *ent.UserUpdateOne) { u.SetStatus(status) })
}

// SetActive flips whether the bot serves this broadcaster. The dashboard
// toggle drives it: inactive users project to standard tier and the ingress
// drops their traffic, so flipping it off silences the channel even before
// the EventSub subscriptions are gone. It is user-re-submittable dashboard
// state, so it goes through the write-behind batcher like every other
// preference; enforcement converges within one flush window.
func (r *Users) SetActive(ctx context.Context, id uint64, active bool) error {
	r.queuePref(id, prefActive, prefWrite{flag: active})
	return nil
}

// SetLocale stores the user's console UI language and announces the change so
// the projector folds the new locale into the Valkey user projection (the
// worker reads it there to answer system commands in the user's language). The
// console's own locale cache is dropped separately via the RPC handler's
// invalidation ping. Write-behind, like the other preferences.
func (r *Users) SetLocale(ctx context.Context, id uint64, locale string) error {
	r.queuePref(id, prefLocale, prefWrite{str: locale})
	return nil
}

// SetCustomCursor stores whether the console shows the animated custom cursor.
// Console-only UI state riding the same write-behind path as the other
// preferences.
func (r *Users) SetCustomCursor(ctx context.Context, id uint64, on bool) error {
	r.queuePref(id, prefCursor, prefWrite{flag: on})
	return nil
}

// SetBanned blocks or unblocks the user from the service. A banned user is
// dropped at the ingress, so their traffic never reaches a worker even if the
// channel is otherwise active. This is a moderation enforcement action with
// low write volume, so it writes through immediately instead of risking a
// flush window on the enforcement path.
func (r *Users) SetBanned(ctx context.Context, id uint64, banned bool) error {
	return r.updateAndPublish(ctx, id, func(u *ent.UserUpdateOne) { u.SetBanned(banned) })
}

// SetCommandsPageHidden turns the public commands page on or off for this
// channel. Write-through, not the queuePref batcher (D8): Get does not overlay
// pending preference writes, and a state_get inside the 2s flush window would
// hand the console a stale row it then caches fresh for 120s -- a race locale
// and cursor hide behind a cookie but a public 404 cannot. Measured against
// SetBanned's precedent: low write volume, correctness over batching.
func (r *Users) SetCommandsPageHidden(ctx context.Context, id uint64, hidden bool) error {
	return r.updateAndPublish(ctx, id, func(u *ent.UserUpdateOne) { u.SetCommandsPageHidden(hidden) })
}

// SetOnboarded marks the user as having finished the onboarding flow.
// Write-behind: the flag is re-derivable from the console session, and losing
// the last window on a process death costs at most a repeated onboarding tour.
func (r *Users) SetOnboarded(ctx context.Context, id uint64, onboarded bool) error {
	r.queuePref(id, prefOnboarded, prefWrite{flag: onboarded})
	return nil
}

// Delete removes the user; tokens cascade away with the row.
func (r *Users) Delete(ctx context.Context, id uint64) error {

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.User.DeleteOneID(id).Exec(ctx)
	}); err != nil {
		return err
	}

	r.views.Invalidate(cache.UserKey(userKeyPrefix, id))

	return bus.PublishJSON(ctx, r.pub, data.SubjectUserDeleted, data.UserDeletedDTO{UserID: id})
}

// Invalidate drops the local cached view; called when a change event arrives
// from another instance of this service.
func (r *Users) Invalidate(id uint64) {
	r.views.Invalidate(cache.UserKey(userKeyPrefix, id))
}

// Close drains pending preference writes and releases the cache's background
// resources.
func (r *Users) Close(ctx context.Context) {
	r.batcher.Close(ctx)
	r.views.Close()
	r.stats.Close()
}

// publishChanged refreshes the local cache view and announces the full new
// state so other instances and the projector converge without querying us.
func (r *Users) publishChanged(ctx context.Context, id uint64) error {

	r.views.Invalidate(cache.UserKey(userKeyPrefix, id))

	view, err := r.Get(ctx, id)
	if err != nil {
		return err
	}

	return bus.PublishJSON(ctx, r.pub, data.SubjectUserChanged, data.UserChangedDTO{
		UserID:             view.ID,
		Username:           view.Username,
		IsActive:           view.IsActive,
		Status:             view.Status,
		Banned:             view.Banned,
		Locale:             view.Locale,
		CommandsPageHidden: view.CommandsPageHidden,
	})
}

func (r *Users) publishStatusInvalidation(_ context.Context, id uint64) error {
	// Core NATS, not JetStream. bagel.cache.invalidate.* has no stream in the
	// catalog (only data.>, twitch, discord, youtube). Publishing here through
	// bus.PublishJSON failed production every 30s with "no stream matches
	// subject bagel.cache.invalidate.status" and left grant projection_phase
	// stuck at pending. Billing and admin already use invalidate.Publish on
	// the RPC connection. Empty nc/prefix is a no-op so tests stay JetStream-free.
	if r.nc == nil || r.invalidationPrefix == "" {
		return nil
	}
	return invalidate.Publish(r.nc, r.invalidationPrefix, "status", strconv.FormatUint(id, 10))
}

// Reproject republishes the current state of every user as ordinary change
// events, paged by ID so the table is never loaded at once. The projector
// requests this on a cold start to rebuild the Valkey projection.
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
		Status: string(row.Status), Banned: row.Banned, Locale: row.Locale,
	})
}

// UpsertToken encrypts and stores an OAuth token. The associated data binds
// the ciphertext to the user, token type and platform, so a ciphertext copied
// onto another row fails authentication on decrypt.
//
// accessTokenExpiresAt is plaintext (a timestamp, not a secret) and optional:
// nil means the caller doesn't know when accessToken expires (the admin
// token-set and dashboard OAuth-callback callers, today) and clears any
// previously stored expiry, because a stale expiry left over from a
// different access token would let a reader wrongly treat the new one as
// still valid. Callers that do know it (outgress's stored-token refresh
// path, from Twitch's expires_in) pass it so Token can serve it back without
// a mint -- see Token's doc and app/twitch/outgress/internal/twitch/token.go.
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

// tokenOwner is a user id in its role as the owner of token rows: match
// selects that user's rows by their foreign key directly, instead of the
// generated edge predicate
// tokens.HasUserWith(user.IDEQ(userID)) every token query here used to carry.
// The edge predicate compiles to a subquery against `users` for what is a
// plain equality on a column `tokens` already holds, which is why these reads
// showed up in the slow log as SELECT ... FROM users. Volume that made it
// worth changing: the outgress broadcaster refresh sweep alone drove ~21,600
// of these reads a day (New Relic, 24h, a flat 15/min on the tokens.get RPC
// verb, ~99% of all users-svc traffic). The FK is not a schema field, so ent
// generates no tokens.UserIDEQ; tokens.UserColumn is the generated name of
// that column, so this stays in step with the schema without hand-written
// SQL strings.
type tokenOwner uint64

func (o tokenOwner) match() predicate.Tokens {
	return predicate.Tokens(func(s *entsql.Selector) {
		s.Where(entsql.EQ(s.C(tokens.UserColumn), uint64(o)))
	})
}

// Token decrypts and returns the stored OAuth token, refresh token, and the
// access token's expiry. Plaintext is returned to the caller and
// deliberately never cached.
//
// accessTokenExpiresAt is nil whenever the row doesn't carry a known expiry
// (see UpsertToken's doc for why that happens) -- ALWAYS treat nil as
// "unknown, not usable", never as "no expiry ever". It is a plaintext
// timestamp read straight off the row; it needs no unpack because, unlike
// accessToken/refreshToken, it was never sealed (see the schema field doc).
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

// applyTokenExpiry sets or clears access_token_expires_at on an update
// builder for one existing token row. Unlike SetNillableAccessTokenExpiresAt
// (used on create, where "unset" already means null), an update must
// actively CLEAR a nil expiry: the row being overwritten may still carry an
// expiry that belonged to the token it is replacing, and leaving that in
// place would let Token hand a caller a TTL for a token that is no longer
// stored here.
func applyTokenExpiry(u *ent.TokensUpdateOne, expiresAt *time.Time) *ent.TokensUpdateOne {
	if expiresAt != nil {
		return u.SetAccessTokenExpiresAt(*expiresAt)
	}
	return u.ClearAccessTokenExpiresAt()
}

// aad is the additional authenticated data binding a sealed token to its
// owner, type and platform.
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
