// Package usersrpc holds the shared wire types for the users service RPC surface.
// These types are transcribed verbatim from app/users/rpc so that consumers can
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
}

// AdminUserView is a single user row in an admin reply.
type AdminUserView struct {
	ID                        uint64     `json:"id"`
	Username                  string     `json:"username"`
	IsActive                  bool       `json:"is_active"`
	Status                    string     `json:"status"`
	Banned                    bool       `json:"banned"`
	SubscriptionExpiresAt     *time.Time `json:"subscription_expires_at,omitempty"`
	SubscriptionSource        string     `json:"subscription_source,omitempty"`
	SubscriptionRef           *string    `json:"subscription_ref,omitempty"`
	SubscriptionCancelPending bool       `json:"subscription_cancel_pending"`
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

// AdminTokenView reports whether a token row is present.
type AdminTokenView struct {
	Present bool `json:"present"`
}

// AdminReply is the reply shape for all admin verbs.
type AdminReply struct {
	User     *AdminUserView  `json:"user,omitempty"`
	Users    []AdminUserView `json:"users,omitempty"`
	Stats    *AdminStats     `json:"stats,omitempty"`
	Token    *AdminTokenView `json:"token,omitempty"`
	Page     int             `json:"page,omitempty"`
	PageSize int             `json:"page_size,omitempty"`
	MaxPages int             `json:"max_pages,omitempty"`
	HasMore  bool            `json:"has_more,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// AuthRequest covers all adminauth verbs.
type AuthRequest struct {
	// actor: who is performing a roster change (set by the console from session).
	ActorID   string `json:"actor_id"`
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

// OnboardedSetRequest is the payload for the dashboard onboarded_set verb.
type OnboardedSetRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Onboarded         bool   `json:"onboarded"`
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
type TokensRequest struct {
	UserID       string `json:"user_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// TokensReply is the reply shape for tokens get/save verbs.
type TokensReply struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Error        string `json:"error,omitempty"`
}
