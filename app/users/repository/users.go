package repository

import (
	"context"
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
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/db"

	"github.com/ThreeDotsLabs/watermill/message"
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

// Users persists the user records and their OAuth tokens. Status reads are
// served from the in-process cache with stampede protection; status writes
// are billing-relevant, so they hit the database directly and are announced
// on the bus right after the commit.
type Users struct {
	client *ent.Client
	views  *cache.Cache[UserView]
	packer domaincrypto.Packer
	pub    message.Publisher
}

func NewUsers(client *ent.Client, packer domaincrypto.Packer, pub message.Publisher) *Users {
	return &Users{
		client: client,
		views:  cache.New[UserView](userCacheCapacity, userCacheTTL),
		packer: packer,
		pub:    pub,
	}
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

// updateAndPublish validates the id, applies a single-field update inside the
// write-through exec, and announces the change so the projector folds it into
// the Valkey user projection. It backs the SetX write-through mutators.
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
// value clears the nullable column.
func (r *Users) SetCreatorCode(ctx context.Context, id uint64, raw string) error {
	code, err := normalizeCreatorCode(raw)
	if err != nil {
		return err
	}
	return r.updateAndPublish(ctx, id, func(u *ent.UserUpdateOne) {
		if code == nil {
			u.ClearCreatorCode()
		} else {
			u.SetCreatorCode(*code)
		}
	})
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
// the EventSub subscriptions are gone.
func (r *Users) SetActive(ctx context.Context, id uint64, active bool) error {
	return r.updateAndPublish(ctx, id, func(u *ent.UserUpdateOne) { u.SetIsActive(active) })
}

// SetLocale stores the user's console UI language and announces the change so
// the projector folds the new locale into the Valkey user projection (the
// worker reads it there to answer system commands in the user's language). The
// console's own locale cache is dropped separately via the RPC handler's
// invalidation ping.
func (r *Users) SetLocale(ctx context.Context, id uint64, locale string) error {
	return r.updateAndPublish(ctx, id, func(u *ent.UserUpdateOne) { u.SetLocale(locale) })
}

// SetCustomCursor stores whether the console shows the animated custom cursor.
// Console-only UI state, but it rides the same write-through + publish path as
// the other preferences so the in-process view cache stays consistent.
func (r *Users) SetCustomCursor(ctx context.Context, id uint64, on bool) error {
	return r.updateAndPublish(ctx, id, func(u *ent.UserUpdateOne) { u.SetCustomCursor(on) })
}

// SetBanned blocks or unblocks the user from the service. A banned user is
// dropped at the ingress, so their traffic never reaches a worker even if the
// channel is otherwise active.
func (r *Users) SetBanned(ctx context.Context, id uint64, banned bool) error {
	return r.updateAndPublish(ctx, id, func(u *ent.UserUpdateOne) { u.SetBanned(banned) })
}

// SetOnboarded marks the user as having finished the onboarding flow.
func (r *Users) SetOnboarded(ctx context.Context, id uint64, onboarded bool) error {
	_, err := r.client.User.UpdateOneID(id).
		SetOnboarded(onboarded).
		Save(ctx)

	if ent.IsNotFound(err) {
		return fmt.Errorf("user not found")
	}
	if err != nil {
		return err
	}

	if err := r.publishChanged(ctx, id); err != nil {
		r.views.Invalidate(cache.UserKey(userKeyPrefix, id))
	}
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

// Close releases the cache's background resources.
func (r *Users) Close() {
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
func (r *Users) UpsertToken(ctx context.Context, userID uint64, tokenType tokens.Type, platform tokens.Platform, accessToken []byte, refreshToken []byte) error {

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
					SetToken(sealed.Ciphertext)

				if len(sealedRefresh.Ciphertext) > 0 {
					create.SetRefreshToken(sealedRefresh.Ciphertext)
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
						update := existing.Update().SetToken(sealed.Ciphertext)
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

			update := existing.Update().SetToken(sealed.Ciphertext)

			if len(sealedRefresh.Ciphertext) > 0 {
				update.SetRefreshToken(sealedRefresh.Ciphertext)
			}

			return update.Exec(ctx)
		})
	})
}

// Token decrypts and returns the stored OAuth token and refresh token.
// Plaintext is returned to the caller and deliberately never cached.
func (r *Users) Token(ctx context.Context, userID uint64, tokenType tokens.Type, platform tokens.Platform) (accessToken []byte, refreshToken []byte, err error) {

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
		return nil, nil, err
	}

	aad := tokenAAD(userID, tokenType, platform)

	accessToken, err = r.packer.Unpack(domaincrypto.SecureEnvelope{Ciphertext: row.Token, AttachedData: aad})
	if err != nil {
		return nil, nil, err
	}

	if len(row.RefreshToken) > 0 {
		if refreshToken, err = r.packer.Unpack(domaincrypto.SecureEnvelope{Ciphertext: row.RefreshToken, AttachedData: aad}); err != nil {
			return nil, nil, err
		}
	}

	return accessToken, refreshToken, nil
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
