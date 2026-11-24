// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import "ItsBagelBot/internal/domain/lifecycle"

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/predicate"
	"ItsBagelBot/app/db/users/ent/tokens"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/tokensign"
	"ItsBagelBot/internal/adminauth"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/consent"
	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/batch"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/tink-crypto/tink-go/v2/tink"

	"go.uber.org/zap"
)

const (
	userKeyPrefix = "user:"

	userCacheTTL = 5 * time.Minute

	userCacheCapacity int64 = 4096
)

// UserView is cached in process, so it must carry no sensitive fields.
type UserView struct {
	Lifecycle                 *lifecycle.State        `json:"lifecycle,omitempty"`
	StateRevision             int64                   `json:"state_revision,omitempty"`
	ID                        uint64                  `json:"id"`
	AccountCreatedAt          int64                   `json:"account_created_at,omitempty"`
	Username                  string                  `json:"username"`
	DisplayName               string                  `json:"display_name"`
	IsActive                  bool                    `json:"is_active"`
	Status                    string                  `json:"status"`
	Banned                    bool                    `json:"banned"`
	Locale                    string                  `json:"locale"`
	CustomCursor              bool                    `json:"custom_cursor"`
	CommandsPageHidden        bool                    `json:"commands_page_hidden"`
	LeaderboardPageHidden     bool                    `json:"leaderboard_page_hidden"`
	SongQueuePageHidden       bool                    `json:"songqueue_page_hidden"`
	CreatorCode               *string                 `json:"creator_code,omitempty"`
	SubscriptionSource        string                  `json:"subscription_source"`
	SubscriptionExpiresAt     *time.Time              `json:"subscription_expires_at,omitempty"`
	SubscriptionRef           *string                 `json:"subscription_ref,omitempty"`
	SubscriptionCancelPending bool                    `json:"subscription_cancel_pending"`
	SubscriptionPaymentFailed bool                    `json:"subscription_payment_failed"`
	Onboarded                 bool                    `json:"onboarded"`
	Alpha                     bool                    `json:"alpha"`
	TermsVersion              string                  `json:"terms_version"`
	MarketingStatus           consent.MarketingStatus `json:"marketing_status"`
}

type Users struct {
	client        *ent.Client
	views         *cache.Cache[UserView]
	stats         *cache.Cache[UserCounts]
	packer        domaincrypto.Packer
	marketingMAC  tink.MAC
	pub           bus.Publisher
	preferences   *batch.Group[prefWrite]
	app           *newrelic.Application
	log           *zap.Logger
	invalidations *bus.Invalidations
	nc            *nats.Conn
	erasures      ErasureStore
	revoker       TokenRevoker
	analytics     *analytics.Recorder
	adminAuth     *adminauth.Client
	deviceSigner  *tokensign.Signer
}

func (r *Users) SetAdminAuth(client *adminauth.Client) { r.adminAuth = client }

func (r *Users) Invalidations() *bus.Invalidations { return r.invalidations }

func (r *Users) SetAnalytics(rec *analytics.Recorder) {
	r.analytics = rec
}

func (r *Users) SetInvalidationConn(nc *nats.Conn) {
	r.nc = nc
}

type UsersConfig struct {
	Client       *ent.Client
	Packer       domaincrypto.Packer
	MarketingMAC tink.MAC
	Pub          bus.Publisher
	App          *newrelic.Application
	Log          *zap.Logger
}

func NewUsers(cfg UsersConfig) *Users {
	client, packer, pub, app, log := cfg.Client, cfg.Packer, cfg.Pub, cfg.App, cfg.Log
	pub, h := bus.WithInvalidations(pub)

	if log == nil {
		log = zap.NewNop()
	}

	r := &Users{
		client:        client,
		views:         cache.New[UserView](userCacheCapacity, userCacheTTL, cache.StaleOnError(userCacheTTL)).InvalidateOn(h, cache.UserKeys(userKeyPrefix), data.SubjectUserChanged, data.SubjectUserDeleted),
		stats:         cache.New[UserCounts](userStatsCapacity, userStatsTTL).InvalidateOn(h, func(cache.Event) []string { return []string{userStatsKey} }, data.SubjectUserChanged, data.SubjectUserDeleted),
		packer:        packer,
		marketingMAC:  cfg.MarketingMAC,
		pub:           pub,
		invalidations: h,
		app:           app,
		log:           log,
	}

	r.preferences = r.newPreferenceGroup()

	return r
}

type displayName string

func (d displayName) storable() displayName {
	if validate.DisplayName(string(d)) != nil {
		return ""
	}
	return d
}

func (d displayName) changes(existing *ent.User, username string) bool {
	if existing.Username != username {
		return true
	}
	return d != "" && existing.DisplayName != string(d)
}

func (d displayName) apply(upd *ent.UserUpdateOne) *ent.UserUpdateOne {
	if d == "" {
		return upd
	}
	return upd.SetDisplayName(string(d))
}

type Registration struct {
	ID          uint64
	Username    string
	DisplayName string
	Email       string
}

func (reg Registration) validate() error {
	if err := validate.UserID(reg.ID); err != nil {
		return err
	}
	if err := validate.Username(reg.Username); err != nil {
		return err
	}
	return validate.Email(reg.Email)
}

func (r *Users) Register(ctx context.Context, request Registration) error {
	id, username, email := request.ID, request.Username, request.Email
	if err := request.validate(); err != nil {
		return err
	}
	name := displayName(request.DisplayName).storable()
	reg := registration{id: id, username: username, email: email, name: name}

	created := false
	if err := db.WithExec(ctx, func(ctx context.Context) error {
		existing, err := r.client.User.Query().
			Where(user.IDEQ(id)).
			Only(ctx)

		switch {
		case ent.IsNotFound(err):
			created, err = r.createUser(ctx, reg)

		case err == nil && name.changes(existing, username):
			_, err = name.apply(existing.Update().SetUsername(username)).
				Save(ctx)
		}

		return err
	}); err != nil {
		return err
	}
	if created {
		r.analytics.Count(catalog.Lifecycle, "signup")
	}

	return r.publishChanged(ctx, id)
}

type registration struct {
	id       uint64
	username string
	email    string
	name     displayName
}

func (r *Users) createUser(ctx context.Context, reg registration) (bool, error) {
	_, err := r.client.User.Create().
		SetID(reg.id).
		SetUsername(reg.username).
		SetDisplayName(string(reg.name)).
		SetEmail(reg.email).
		SetLifecycle(&lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, AdmissionGeneration: 1, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy, LastEngagedAt: time.Now().UTC()}).
		SetLifecycleState(lifecycle.Active).
		SetDormantAfter(time.Now().UTC().Add(lifecycle.DormancyPeriod)).
		Save(ctx)
	if ent.IsConstraintError(err) {
		_, err = reg.name.apply(r.client.User.UpdateOneID(reg.id).SetUsername(reg.username)).Save(ctx)
		return false, err
	}
	return err == nil, err
}

func (r *Users) Get(ctx context.Context, id uint64) (UserView, error) {
	key := cache.UserKey(userKeyPrefix, id)
	return r.views.GetOrLoad(ctx, key, func(ctx context.Context) (UserView, error) {
		return r.loadUser(ctx, id)
	})
}

func (r *Users) loadUser(ctx context.Context, id uint64) (UserView, error) {
	return db.WithRead(ctx, func(ctx context.Context) (UserView, error) {

		u, err := r.client.User.Query().
			Where(user.IDEQ(id)).
			Only(ctx)
		if ent.IsNotFound(err) {
			// Drop the view so StaleOnError cannot resurrect a deleted user.
			r.views.Invalidate(cache.UserKey(userKeyPrefix, id))
		}
		if err != nil {
			return UserView{}, err
		}

		return userView(u), nil
	})
}

func userView(u *ent.User) UserView {
	return UserView{
		Lifecycle:                 lifecycleView(u),
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
		LeaderboardPageHidden:     u.LeaderboardPageHidden,
		SongQueuePageHidden:       u.SongqueuePageHidden,
		CreatorCode:               u.CreatorCode,
		SubscriptionSource:        u.SubscriptionSource,
		SubscriptionExpiresAt:     u.SubscriptionExpiresAt,
		SubscriptionRef:           u.SubscriptionRef,
		SubscriptionCancelPending: u.SubscriptionCancelPending,
		SubscriptionPaymentFailed: u.SubscriptionPaymentFailed,
		Onboarded:                 u.Onboarded,
		Alpha:                     u.Alpha,
		TermsVersion:              u.TermsVersion,
		MarketingStatus:           consent.MarketingStatus(u.MarketingStatus),
	}
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

// SetStatus is on the money path and must persist synchronously.
func (r *Users) SetStatus(ctx context.Context, id uint64, status user.Status) error {
	if err := validate.Status(string(status)); err != nil {
		return err
	}
	return r.updateAndPublish(ctx, id, func(u *ent.UserUpdateOne) { u.SetStatus(status) })
}

func (r *Users) SetActive(ctx context.Context, id uint64, active bool) error {
	return r.setDesiredEnabled(ctx, id, active)
}

func (r *Users) SetActiveNow(ctx context.Context, id uint64, active bool) error {
	return r.SetActive(ctx, id, active)
}

// Commit bans and revoke desktop grants even if the revocation publish fails.
func (r *Users) SetBanned(ctx context.Context, id uint64, banned bool) error {
	banTime := time.Now()
	if err := r.updateAndPublish(ctx, id, func(u *ent.UserUpdateOne) { u.SetBanned(banned) }); err != nil {
		return err
	}
	if !banned {
		return nil
	}
	if _, err := r.RevokeAllDeviceGrants(ctx, id, "ban"); err != nil {
		r.log.Warn("device grant revoke on ban failed", zap.Uint64("user_id", id), zap.Error(err))
	}
	return bus.PublishJSON(ctx, r.pub, data.SubjectUserBanned, data.UserBannedDTO{UserID: id, BannedAt: banTime})
}

type PublicPage string

const (
	PageCommands    PublicPage = "commands"
	PageLeaderboard PublicPage = "leaderboard"
	PageSongQueue   PublicPage = "songqueue"
)

var pageHiddenSetters = map[PublicPage]func(*ent.UserUpdateOne, bool){
	PageCommands:    func(u *ent.UserUpdateOne, hidden bool) { u.SetCommandsPageHidden(hidden) },
	PageLeaderboard: func(u *ent.UserUpdateOne, hidden bool) { u.SetLeaderboardPageHidden(hidden) },
	PageSongQueue:   func(u *ent.UserUpdateOne, hidden bool) { u.SetSongqueuePageHidden(hidden) },
}

// Must write through: Get does not overlay pending preference writes.
func (r *Users) SetPageHidden(ctx context.Context, id uint64, page PublicPage, hidden bool) error {
	set, ok := pageHiddenSetters[page]
	if !ok {
		return fmt.Errorf("unknown public page %q", page)
	}
	return r.updateAndPublish(ctx, id, func(u *ent.UserUpdateOne) { set(u, hidden) })
}

func (r *Users) Delete(ctx context.Context, id uint64) error {
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.User, error) { return r.client.User.Get(ctx, id) })
	if err != nil {
		return err
	}
	if err := r.recordErasure(ctx, erasureRef{userID: id, accountCreatedAt: row.CreatedAt.UnixMicro()}); err != nil {
		return fmt.Errorf("record erasure: %w", err)
	}
	return r.erase(ctx, id, row.CreatedAt)
}

func (r *Users) erase(ctx context.Context, id uint64, createdAt time.Time) error {
	r.revokeTwitchTokens(ctx, id)
	if _, err := r.RevokeAllDeviceGrants(ctx, id, "erasure"); err != nil {
		r.log.Warn("device grant revoke on erasure failed", zap.Uint64("user_id", id), zap.Error(err))
	}
	deleted := data.UserDeletedDTO{UserID: id, AccountCreatedAt: createdAt.UnixMicro()}

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		return db.WithTx(ctx, r.client, func(tx *ent.Tx) error {
			row, err := tx.User.Get(ctx, id)
			if err != nil {
				return err
			}
			erasure, err := tx.LifecycleErasure.Create().SetUserID(id).SetAccountCreatedAt(deleted.AccountCreatedAt).SetLifecycle(lifecycleView(row)).Save(ctx)
			if err != nil {
				return err
			}
			deleted.DeletedAt = erasure.DeletedAt
			return eraseAccountRow(ctx, tx, id, createdAt)
		})
	}); err != nil {
		return err
	}

	r.views.Invalidate(cache.UserKey(userKeyPrefix, id))
	r.analytics.Count(catalog.Lifecycle, "account_deleted")

	return r.publishDeletions(ctx, []data.UserDeletedDTO{deleted})
}

func eraseAccountRow(ctx context.Context, tx *ent.Tx, id uint64, createdAt time.Time) error {
	return tx.User.DeleteOneID(id).Where(user.CreatedAtEQ(createdAt)).Exec(ctx)
}

func (r *Users) publishDeleted(ctx context.Context, deleted data.UserDeletedDTO) error {
	return errors.Join(
		bus.PublishJSON(ctx, r.pub, data.SubjectUserDeleted, deleted),
		r.invalidations.Publish(invalidate.ScopeStatus, deleted.UserID),
		r.invalidations.Publish(invalidate.ScopeUser, deleted.UserID),
	)
}

func (r *Users) Invalidate(id uint64) {
	r.views.Invalidate(cache.UserKey(userKeyPrefix, id))
}

func (r *Users) Close(ctx context.Context) {
	if err := r.preferences.Close(ctx); err != nil {
		r.log.Error("preference drain failed", zap.Error(err))
	}
	r.views.Close()
	r.stats.Close()
}

func (r *Users) publishChanged(ctx context.Context, id uint64) error {
	view, err := r.loadUser(ctx, id)
	if err != nil {
		r.Invalidate(id)
		return err
	}

	err = bus.PublishJSON(ctx, r.pub, data.SubjectUserChanged, changedDTO(view))
	r.invalidations.Publish(invalidate.ScopeStatus, id)
	return err
}

func changedDTO(view UserView) data.UserChangedDTO {
	return data.UserChangedDTO{
		Lifecycle:          view.Lifecycle,
		VoicePlan:          string(voiceplan.PlanFor(view.Status, view.SubscriptionSource, view.IsActive, view.Banned)),
		UserID:             view.ID,
		AccountCreatedAt:   view.AccountCreatedAt,
		StateRevision:      view.StateRevision,
		Username:           view.Username,
		IsActive:           view.IsActive,
		Status:             view.Status,
		Banned:             view.Banned,
		Locale:             view.Locale,
		CommandsPageHidden: view.CommandsPageHidden,

		LeaderboardPageHidden: view.LeaderboardPageHidden,
		SongQueuePageHidden:   view.SongQueuePageHidden,
		CreatorCode:           view.CreatorCodeText(),
		Alpha:                 view.Alpha,
	}
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

		if err := r.publishReprojectedPage(ctx, rows); err != nil {
			return err
		}

		if len(rows) < pageSize {
			return nil
		}

		afterID = rows[len(rows)-1].ID
	}
}

func (r *Users) publishReprojectedPage(ctx context.Context, rows []*ent.User) error {
	for _, row := range rows {
		if err := r.publishReprojectedUser(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

func (r *Users) publishReprojectedUser(ctx context.Context, row *ent.User) error {
	return bus.PublishJSON(ctx, r.pub, data.SubjectUserChanged, data.UserChangedDTO{
		Lifecycle: row.Lifecycle,
		VoicePlan: string(voiceplan.PlanFor(string(row.Status), row.SubscriptionSource, row.IsActive, row.Banned)),
		UserID:    row.ID, Username: row.Username, IsActive: row.IsActive,
		AccountCreatedAt: row.CreatedAt.UnixMicro(),
		StateRevision:    row.StateRevision,
		Status:           string(row.Status), Banned: row.Banned, Locale: row.Locale,
		CreatorCode: UserView{CreatorCode: row.CreatorCode}.CreatorCodeText(),
	})
}

type TokenWrite struct {
	UserID        uint64
	Type          tokens.Type
	Platform      tokens.Platform
	AccessToken   []byte
	RefreshToken  []byte
	ExpiresAt     *time.Time
	Scopes        []string
	ReplaceScopes bool
}

func (w TokenWrite) validate() error {
	if err := validate.UserID(w.UserID); err != nil {
		return err
	}
	if err := validate.Token(w.AccessToken); err != nil {
		return err
	}
	if len(w.RefreshToken) == 0 {
		return nil
	}
	return validate.Token(w.RefreshToken)
}

type sealedToken struct {
	TokenWrite
	access  domaincrypto.SecureEnvelope
	refresh domaincrypto.SecureEnvelope
}

func (r *Users) UpsertToken(ctx context.Context, write TokenWrite) error {
	if err := write.validate(); err != nil {
		return err
	}
	sealed, err := r.sealToken(write)
	if err != nil {
		return err
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		return db.WithTx(ctx, r.client, func(tx *ent.Tx) error { return sealed.save(ctx, tx) })
	})
}

func (r *Users) sealToken(write TokenWrite) (sealedToken, error) {
	s := sealedToken{TokenWrite: write}
	aad := tokenOwner(write.UserID).aad(write.Type, write.Platform)
	var err error
	s.access, err = r.packer.Pack(write.AccessToken, aad)
	if err != nil || len(write.RefreshToken) == 0 {
		return s, err
	}
	s.refresh, err = r.packer.Pack(write.RefreshToken, aad)
	return s, err
}

func (s sealedToken) find(ctx context.Context, tx *ent.Tx) (*ent.Tokens, error) {
	return tx.Tokens.Query().Where(tokens.TypeEQ(s.Type), tokens.PlatformEQ(s.Platform), tokenOwner(s.UserID).match()).Only(ctx)
}

func (s sealedToken) save(ctx context.Context, tx *ent.Tx) error {
	existing, err := s.find(ctx, tx)
	if ent.IsNotFound(err) {
		return s.create(ctx, tx)
	}
	if err != nil {
		return err
	}
	return s.update(ctx, existing)
}

func (s sealedToken) create(ctx context.Context, tx *ent.Tx) error {
	create := tx.Tokens.Create().SetUserID(s.UserID).SetType(s.Type).SetPlatform(s.Platform).SetToken(s.access.Ciphertext).SetNillableAccessTokenExpiresAt(s.ExpiresAt)
	if len(s.refresh.Ciphertext) > 0 {
		create.SetRefreshToken(s.refresh.Ciphertext)
	}
	if s.Scopes != nil {
		create.SetScopes(s.Scopes)
	}
	err := create.Exec(ctx)
	if !ent.IsConstraintError(err) {
		return err
	}
	existing, err := s.find(ctx, tx)
	if err != nil {
		return err
	}
	return s.update(ctx, existing)
}

func (s sealedToken) update(ctx context.Context, existing *ent.Tokens) error {
	update := applyTokenExpiry(existing.Update().SetToken(s.access.Ciphertext), s.ExpiresAt)
	if len(s.refresh.Ciphertext) > 0 {
		update.SetRefreshToken(s.refresh.Ciphertext)
	}
	switch {
	case s.Scopes != nil:
		update.SetScopes(s.Scopes)
	case s.ReplaceScopes:
		update.ClearScopes()
	}
	return update.Exec(ctx)
}

type tokenOwner uint64

func (o tokenOwner) match() predicate.Tokens {
	return predicate.Tokens(func(s *entsql.Selector) {
		s.Where(entsql.EQ(s.C(tokens.UserColumn), uint64(o)))
	})
}

type StoredToken struct {
	AccessToken  []byte
	RefreshToken []byte
	ExpiresAt    *time.Time
	Scopes       []string
}

func (r *Users) Token(ctx context.Context, userID uint64, tokenType tokens.Type, platform tokens.Platform) (StoredToken, error) {
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
		return StoredToken{}, err
	}

	aad := tokenOwner(userID).aad(tokenType, platform)

	accessToken, err := r.packer.Unpack(domaincrypto.SecureEnvelope{Ciphertext: row.Token, AttachedData: aad})
	if err != nil {
		return StoredToken{}, err
	}

	token := StoredToken{AccessToken: accessToken, ExpiresAt: row.AccessTokenExpiresAt, Scopes: row.Scopes}
	if len(row.RefreshToken) == 0 {
		return token, nil
	}
	token.RefreshToken, err = r.packer.Unpack(domaincrypto.SecureEnvelope{Ciphertext: row.RefreshToken, AttachedData: aad})
	if err != nil {
		return StoredToken{}, err
	}
	return token, nil
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
