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
	"unicode/utf8"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/tokens"
	"ItsBagelBot/app/users/ent/user"
	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/batch"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/db"

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
	IsActive                  bool       `json:"is_active"`
	Status                    string     `json:"status"`
	Banned                    bool       `json:"banned"`
	Locale                    string     `json:"locale"`
	CustomCursor              bool       `json:"custom_cursor"`
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
	client  *ent.Client
	views   *cache.Cache[UserView]
	packer  domaincrypto.Packer
	pub     bus.Publisher
	batcher *batch.Batcher[prefKey, prefWrite]
	app     *newrelic.Application
	log     *zap.Logger
}

func NewUsers(client *ent.Client, packer domaincrypto.Packer, pub bus.Publisher, app *newrelic.Application, log *zap.Logger) *Users {

	if log == nil {
		log = zap.NewNop()
	}

	r := &Users{
		client: client,
		views:  cache.New[UserView](userCacheCapacity, userCacheTTL),
		packer: packer,
		pub:    pub,
		app:    app,
		log:    log,
	}

	r.batcher = batch.New[prefKey, prefWrite](prefsFlushInterval, prefsFlushMaxSize, r.flushPrefs, log)

	return r
}

// Register creates the user on first sight and refreshes the username on
// conflict, so a re-login after a Twitch rename converges automatically.
func (r *Users) Register(ctx context.Context, id uint64, username string, email string) error {

	if err := validate.UserID(id); err != nil {
		return err
	}
	if err := validate.Username(username); err != nil {
		return err
	}
	if err := validate.Email(email); err != nil {
		return err
	}

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		existing, err := r.client.User.Query().
			Where(user.IDEQ(id)).
			Only(ctx)

		switch {
		case ent.IsNotFound(err):
			_, err = r.client.User.Create().
				SetID(id).
				SetUsername(username).
				SetEmail(email).
				Save(ctx)
			if ent.IsConstraintError(err) {
				_, err = r.client.User.UpdateOneID(id).
					SetUsername(username).
					Save(ctx)
			}

		case err == nil && existing.Username != username:
			_, err = existing.Update().
				SetUsername(username).
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
				IsActive:                  u.IsActive,
				Status:                    string(u.Status),
				Banned:                    u.Banned,
				Locale:                    u.Locale,
				CustomCursor:              u.CustomCursor,
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
		UserID:   view.ID,
		Username: view.Username,
		IsActive: view.IsActive,
		Status:   view.Status,
		Banned:   view.Banned,
		Locale:   view.Locale,
	})
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
			if err := bus.PublishJSON(ctx, r.pub, data.SubjectUserChanged, data.UserChangedDTO{
				UserID:   row.ID,
				Username: row.Username,
				IsActive: row.IsActive,
				Status:   string(row.Status),
				Banned:   row.Banned,
				Locale:   row.Locale,
			}); err != nil {
				return err
			}
		}

		if len(rows) < pageSize {
			return nil
		}

		afterID = rows[len(rows)-1].ID
	}
}

// UpsertToken encrypts and stores an OAuth token. The associated data binds
// the ciphertext to the user, token type and platform, so a ciphertext copied
// onto another row fails authentication on decrypt.
//
// accessTokenExpiresAt is plaintext (a timestamp, not a secret) and optional:
// nil means the caller doesn't know when accessToken expires (the admin
// token-set path, today) and clears any previously stored expiry, because a
// stale expiry left over from a different access token would let a reader
// wrongly treat the new one as still valid. Callers that do know it pass it so
// Token can serve it back without a mint -- see Token's doc.
//
// Options carry platform-specific extras without widening the positional
// signature every twitch caller compiles against; WithYouTubeChannelID is the
// only one today.
func (r *Users) UpsertToken(ctx context.Context, userID uint64, tokenType tokens.Type, platform tokens.Platform, accessToken []byte, refreshToken []byte, accessTokenExpiresAt *time.Time, opts ...TokenOption) error {

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

	var extra tokenExtra
	for _, opt := range opts {
		opt(&extra)
	}
	if extra.youtubeChannelID != "" {
		if platform != tokens.PlatformYoutube {
			return ErrChannelIDOnNonYouTube
		}
		if err := ValidateYouTubeChannelID(extra.youtubeChannelID); err != nil {
			return err
		}
	}

	aad := tokenAAD(userID, tokenType, platform)

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
					tokens.HasUserWith(user.IDEQ(userID)),
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
				if extra.youtubeChannelID != "" {
					create.SetYoutubeChannelID(extra.youtubeChannelID)
				}

				if err := create.Exec(ctx); err != nil {
					if ent.IsConstraintError(err) {
						existing, err = tx.Tokens.Query().
							Where(
								tokens.TypeEQ(tokenType),
								tokens.PlatformEQ(platform),
								tokens.HasUserWith(user.IDEQ(userID)),
							).
							Only(ctx)
						if err != nil {
							return err
						}
						update := applyTokenExpiry(existing.Update().SetToken(sealed.Ciphertext), accessTokenExpiresAt)
						if len(sealedRefresh.Ciphertext) > 0 {
							update.SetRefreshToken(sealedRefresh.Ciphertext)
						}
						if extra.youtubeChannelID != "" {
							update.SetYoutubeChannelID(extra.youtubeChannelID)
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
			if extra.youtubeChannelID != "" {
				update.SetYoutubeChannelID(extra.youtubeChannelID)
			}

			return update.Exec(ctx)
		})
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
				tokens.HasUserWith(user.IDEQ(userID)),
			).
			Only(ctx)
	})
	if err != nil {
		return nil, nil, nil, err
	}

	aad := tokenAAD(userID, tokenType, platform)

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

// TokenOption carries optional per-platform extras into UpsertToken without
// widening the positional signature every existing twitch caller compiles
// against. Zero-value (no options) is exactly the pre-youtube behaviour.
type TokenOption func(*tokenExtra)

type tokenExtra struct {
	youtubeChannelID string
}

// WithYouTubeChannelID records which YouTube channel a Google grant speaks
// for, making the row resolvable by the token lease RPC (which is addressed
// by channel id -- consumers never learn our internal user ids). Re-linking
// the same user overwrites the column; linking a channel another user
// already holds fails on the unique index rather than silently stealing the
// lease mapping.
func WithYouTubeChannelID(channelID string) TokenOption {
	return func(x *tokenExtra) { x.youtubeChannelID = channelID }
}

// ErrChannelIDOnNonYouTube guards against a caller attaching a YouTube
// channel to a twitch grant -- always a bug in the caller, never a state we
// should persist.
var ErrChannelIDOnNonYouTube = errors.New("youtube_channel_id is only valid for platform=youtube")

// ValidateYouTubeChannelID accepts the opaque "UC..." ids Google mints and
// rejects anything else early enough that a bad console payload can't write
// an unresolvable row (a row with a garbage id would be invisible to the
// lease RPC forever). Length-capped at 64: real ids are 24 chars, but Google
// treats them as opaque strings and has widened formats before; only the
// prefix is load-bearing for lookup confidence, so the rest is length and
// printable-ASCII hygiene rather than format enforcement.
func ValidateYouTubeChannelID(id string) error {
	if len(id) < 3 || len(id) > 64 {
		return ErrYouTubeChannelInvalid
	}
	if !strings.HasPrefix(id, "UC") {
		return ErrYouTubeChannelInvalid
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c < 0x20 || c > 0x7e {
			return ErrYouTubeChannelInvalid
		}
	}
	return nil
}

var ErrYouTubeChannelInvalid = errors.New("invalid youtube channel id")

// TokenByYouTubeChannel resolves the youtube grant row owning a channel and
// decrypts it. It backs bagel.rpc.youtube.token.get, whose request keys off
// channel id alone: consumers hold no notion of our internal user ids, so
// this is the only path from a wire request to a credential.
//
// The returned userID is required by callers that re-persist rotated access
// tokens (UpsertToken's AAD binds ciphertexts to the user), and refreshToken/
// accessTokenExpiresAt follow exactly Token's semantics -- nil expiry means
// "unknown, treat as expired".
func (r *Users) TokenByYouTubeChannel(ctx context.Context, channelID string) (userID uint64, accessToken []byte, refreshToken []byte, accessTokenExpiresAt *time.Time, err error) {
	if err := ValidateYouTubeChannelID(channelID); err != nil {
		return 0, nil, nil, nil, err
	}

	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.Tokens, error) {
		return r.client.Tokens.Query().
			Where(
				tokens.TypeEQ(tokens.TypeUserToken),
				tokens.PlatformEQ(tokens.PlatformYoutube),
				tokens.YoutubeChannelIDEQ(channelID),
			).
			Only(ctx)
	})
	if err != nil {
		return 0, nil, nil, nil, err
	}

	// The owning user id rides the edge (the generated struct keeps the FK
	// private); OnlyID fetches just the column.
	userID, err = row.QueryUser().OnlyID(ctx)
	if err != nil {
		return 0, nil, nil, nil, err
	}

	aad := tokenAAD(userID, tokens.TypeUserToken, tokens.PlatformYoutube)

	accessToken, err = r.packer.Unpack(domaincrypto.SecureEnvelope{Ciphertext: row.Token, AttachedData: aad})
	if err != nil {
		return 0, nil, nil, nil, err
	}

	if len(row.RefreshToken) > 0 {
		if refreshToken, err = r.packer.Unpack(domaincrypto.SecureEnvelope{Ciphertext: row.RefreshToken, AttachedData: aad}); err != nil {
			return 0, nil, nil, nil, err
		}
	}

	return userID, accessToken, refreshToken, row.AccessTokenExpiresAt, nil
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

func tokenAAD(userID uint64, tokenType tokens.Type, platform tokens.Platform) []byte {

	aad := make([]byte, 0, 20+1+len(tokenType)+1+len(platform))

	aad = strconv.AppendUint(aad, userID, 10)
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
