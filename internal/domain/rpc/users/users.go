// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package usersrpc holds the shared wire types for the users service RPC surface.
// These types are transcribed verbatim from app/db/users/rpc so that consumers can
// reference a single, import-friendly package without pulling in the full service.
package usersrpc

import "time"

// AdminRequest covers all admin verbs; unused fields are zero-valued.
type AdminRequest struct {
	UserID       string `json:"user_id"`
	Username     string `json:"username"`
	Status       string `json:"status"`
	Active       bool   `json:"active"`
	Limit        int    `json:"limit"`
	Page         int    `json:"page"`
	Search       string `json:"search"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	CreatorCode  string `json:"creator_code,omitempty"`
	// Days is the trailing window size for the enrollment verb (UTC days,
	// today included). Zero means the server default.
	Days int `json:"days,omitempty"`
	// State filters list/overview to one effective user state: vip, paid,
	// free, banned, or inactive. Empty means no filter. Precedence matches
	// the console: banned beats inactive beats tier.
	State string `json:"state,omitempty"`
}

// AdminUserView is a single user row in an admin reply.
type AdminUserView struct {
	ID                        uint64     `json:"id"`
	Username                  string     `json:"username"`
	IsActive                  bool       `json:"is_active"`
	Status                    string     `json:"status"`
	Banned                    bool       `json:"banned"`
	CreatorCode               *string    `json:"creator_code,omitempty"`
	SubscriptionExpiresAt     *time.Time `json:"subscription_expires_at,omitempty"`
	SubscriptionSource        string     `json:"subscription_source,omitempty"`
	SubscriptionRef           *string    `json:"subscription_ref,omitempty"`
	SubscriptionCancelPending bool       `json:"subscription_cancel_pending"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

// AdminStats aggregates user counts for the admin overview.
type AdminStats struct {
	TotalUsers   int `json:"total_users"`
	ActiveUsers  int `json:"active_users"`
	PremiumUsers int `json:"premium_users"`
	VIPUsers     int `json:"vip_users"`
	PaidUsers    int `json:"paid_users"`
}

// AdminEnrollmentDay is one UTC day's signup count.
type AdminEnrollmentDay struct {
	Date  string `json:"date"` // YYYY-MM-DD
	Count int    `json:"count"`
}

// AdminEnrollmentView carries the daily signup histogram plus the current
// user totals, so consoles can chart signups against the registered base.
type AdminEnrollmentView struct {
	Days  []AdminEnrollmentDay `json:"days"`
	Stats AdminStats           `json:"stats"`
}

// AdminTokenView reports whether a token row is present.
type AdminTokenView struct {
	Present bool `json:"present"`
}

// AdminReply is the reply shape for all admin verbs.
type AdminReply struct {
	User       *AdminUserView       `json:"user,omitempty"`
	Users      []AdminUserView      `json:"users,omitempty"`
	Stats      *AdminStats          `json:"stats,omitempty"`
	Enrollment *AdminEnrollmentView `json:"enrollment,omitempty"`
	Token      *AdminTokenView      `json:"token,omitempty"`
	Page       int                  `json:"page,omitempty"`
	PageSize   int                  `json:"page_size,omitempty"`
	MaxPages   int                  `json:"max_pages,omitempty"`
	HasMore    bool                 `json:"has_more,omitempty"`
	Error      string               `json:"error,omitempty"`
}

// AuthRequest covers all adminauth verbs.
type AuthRequest struct {
	// ActorID identifies who is performing a roster change. The users service
	// resolves this actor's active role from its own database.
	ActorID string `json:"actor_id"`
	// ActorRole is retained for wire compatibility and spoofing regression
	// tests only. It is client-supplied and must never be used for authorization.
	ActorRole string `json:"actor_role"`

	// target / identity
	UserID      string `json:"user_id"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`

	// audit.append
	ActorLogin string `json:"actor_login"`
	Action     string `json:"action"`
	Target     string `json:"target"`
	Detail     string `json:"detail"`
	OK         bool   `json:"ok"`
	Err        string `json:"error"`

	// audit.list / auth.list
	Limit       int    `json:"limit"`
	ActorFilter string `json:"actor_filter"`
	Page        int    `json:"page"`
	Search      string `json:"search"`
}

// AdminAcctView is a single staff member row.
type AdminAcctView struct {
	ID          uint64    `json:"id"`
	Login       string    `json:"login"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	Active      bool      `json:"active"`
	AddedBy     uint64    `json:"added_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// AuditView is a single audit log entry.
type AuditView struct {
	ID         int       `json:"id"`
	ActorID    uint64    `json:"actor_id"`
	ActorLogin string    `json:"actor_login"`
	Action     string    `json:"action"`
	Target     string    `json:"target,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	OK         bool      `json:"ok"`
	Err        string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// AuthReply is the reply shape for all adminauth verbs.
type AuthReply struct {
	Admin       bool            `json:"admin"`
	Role        string          `json:"role,omitempty"`
	Login       string          `json:"login,omitempty"`
	DisplayName string          `json:"display_name,omitempty"`
	Admins      []AdminAcctView `json:"admins,omitempty"`
	Entries     []AuditView     `json:"entries,omitempty"`
	Page        int             `json:"page,omitempty"`
	PageSize    int             `json:"page_size,omitempty"`
	MaxPages    int             `json:"max_pages,omitempty"`
	HasMore     bool            `json:"has_more,omitempty"`
	Error       string          `json:"error,omitempty"`
}

// UpsertUserRequest is the payload for the dashboard upsert_user verb.
type UpsertUserRequest struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	// Email is the real Twitch account email (user:read:email), forwarded by
	// the dashboard login callback when Twitch returns one. Optional:
	// registration proceeds without it, and storage failure never blocks a
	// session. Stored encrypted at rest, only for transactional mail.
	Email string `json:"email,omitempty"`
}

// LoginResolveRequest asks the users service to map a Twitch login to the
// broadcaster id behind it. It backs the public command page's /user/<login>
// URL: the login is only a lookup key, and the id it returns is what selects
// the commands rendered.
type LoginResolveRequest struct {
	Login string `json:"login"`
}

// LoginResolveReply carries the resolved id plus the stored (canonical-casing)
// login, which the page redirects to so a channel has a single URL.
type LoginResolveReply struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Error    string `json:"error,omitempty"`
}

// EmailGetRequest asks for a user's decrypted contact email. Internal-only:
// the subject is export/import-scoped on the NATS account level so only
// services that send transactional mail can call it.
type EmailGetRequest struct {
	UserID string `json:"user_id"`
}

// EmailGetReply carries the contact email or a terminal error. An empty Email
// with empty Error means the user has none on record yet.
type EmailGetReply struct {
	Email string `json:"email,omitempty"`
	Error string `json:"error,omitempty"`
}

// CountsRequest asks for the public enrollment counts. Empty: unlike the
// admin surface's stats verb, this endpoint takes no filters -- it exists to
// answer exactly one question (how many streams total, how many active) for
// callers that must not see anything more, so there is nothing to parameterize.
type CountsRequest struct{}

// CountsReply carries the narrow public subset of repository.UserStats:
// enough for a cosmetic count (Discord bot presence, a public counter) and
// nothing that identifies who those users are. This deliberately excludes
// PremiumUsers/VIPUsers/PaidUsers -- see bagel.rpc.internal.users.counts.get's
// doc for why those stay behind the admin-authenticated stats verb.
type CountsReply struct {
	TotalUsers  int    `json:"total_users"`
	ActiveUsers int    `json:"active_users"`
	Error       string `json:"error,omitempty"`
}

// GrantSaveRequest is the payload for the dashboard grant_save verb.
type GrantSaveRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	AccessToken       string `json:"access_token"`
	RefreshToken      string `json:"refresh_token"`
}

// GrantHasRequest is the payload for the dashboard grant_has verb.
type GrantHasRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

// ActiveSetRequest is the payload for the dashboard active_set verb.
type ActiveSetRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Active            bool   `json:"active"`
}

// DeleteSelfRequest is the payload for the dashboard delete_self verb.
type DeleteSelfRequest struct {
	UserID string `json:"user_id"`
}

// LocaleSetRequest is the payload for the dashboard locale_set verb.
type LocaleSetRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Locale            string `json:"locale"`
}

// StateGetRequest is the payload for the dashboard state_get verb.
type StateGetRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

// StateGetReply is the subset of the state_get reply Go callers consume
// (outgress reads the locale to localize streamer-facing reauth copy). The
// verb returns more fields; unknown ones are ignored on decode.
type StateGetReply struct {
	Active    bool   `json:"active"`
	Status    string `json:"status"`
	Onboarded bool   `json:"onboarded"`
	Locale    string `json:"locale"`
	Error     string `json:"error,omitempty"`
}

// OnboardedSetRequest is the payload for the dashboard onboarded_set verb.
type OnboardedSetRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Onboarded         bool   `json:"onboarded"`
}

// CursorSetRequest is the payload for the dashboard cursor_set verb.
type CursorSetRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	CustomCursor      bool   `json:"custom_cursor"`
}

// CreateDelegationRequest is the payload for the delegation create verb.
type CreateDelegationRequest struct {
	OwnerUserID string   `json:"owner_user_id"`
	OwnerLogin  string   `json:"owner_login"`
	Sections    []string `json:"sections"`
}

// TokenRequest is the payload for the delegation get verb.
type TokenRequest struct {
	Token string `json:"token"`
}

// ConsumeDelegationRequest is the payload for the delegation consume verb.
type ConsumeDelegationRequest struct {
	Token          string `json:"token"`
	DelegateUserID string `json:"delegate_user_id"`
	DelegateLogin  string `json:"delegate_login"`
}

// OwnerRequest is the payload for the delegation list verb.
type OwnerRequest struct {
	OwnerUserID string `json:"owner_user_id"`
}

// RevokeDelegationRequest is the payload for the delegation revoke verb.
type RevokeDelegationRequest struct {
	OwnerUserID string `json:"owner_user_id"`
	Token       string `json:"token"`
}

// UpdateDelegationRequest changes the granted sections of an existing grant
// (pending or consumed), scoped to its owner.
type UpdateDelegationRequest struct {
	OwnerUserID string   `json:"owner_user_id"`
	Token       string   `json:"token"`
	Sections    []string `json:"sections"`
}

// AccessRequest is the payload for the delegation access verb.
type AccessRequest struct {
	DelegateUserID string `json:"delegate_user_id"`
}

// OptOutDelegationRequest is the payload for the delegation opt_out verb.
type OptOutDelegationRequest struct {
	OwnerUserID    string `json:"owner_user_id"`
	DelegateUserID string `json:"delegate_user_id"`
}

// TokensRequest is the payload for the tokens get/save verbs.
//
// AccessTokenExpiresAt travels alongside AccessToken on "save" so the store
// can serve the access token back on a later "get" instead of every reader
// re-minting one from RefreshToken (see TokensReply.AccessTokenExpiresAt for
// why that matters). Nil means "caller doesn't know the expiry" -- the admin
// tokenSet and dashboard grant_save paths still save without it, and that is
// unchanged behaviour, not a regression.
type TokensRequest struct {
	UserID               string     `json:"user_id"`
	AccessToken          string     `json:"access_token"`
	RefreshToken         string     `json:"refresh_token"`
	AccessTokenExpiresAt *time.Time `json:"access_token_expires_at,omitempty"`
}

// TokensReply is the reply shape for tokens get/save verbs.
//
// AccessTokenExpiresAt is the stored access token's absolute UTC expiry, so a
// "get" caller can decide whether AccessToken is still usable without ever
// touching id.twitch.tv. A nil value means "unknown expiry" (either the row
// predates this field, or was written by a caller that didn't supply one) and
// MUST be treated as "not usable" by callers deciding whether to adopt the
// token -- never as "never expires". A reply from a users service build that
// predates this field simply omits both new-ish fields, which is exactly the
// "unknown expiry" case and therefore requires no version check on the
// caller's side: see twitch.NewStoredUserTokenSource in
// app/twitch/outgress/internal/twitch/token.go, which falls back to minting from
// RefreshToken whenever AccessTokenExpiresAt is nil or too close to now.
type TokensReply struct {
	AccessToken          string     `json:"access_token,omitempty"`
	RefreshToken         string     `json:"refresh_token,omitempty"`
	AccessTokenExpiresAt *time.Time `json:"access_token_expires_at,omitempty"`
	Error                string     `json:"error,omitempty"`
}
