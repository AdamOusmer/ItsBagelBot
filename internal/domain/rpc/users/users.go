// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package usersrpc

import "ItsBagelBot/internal/domain/rpc"

import "time"

type AdminRequest struct {
	ActorID      string `json:"actor_id"`
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
	Days         int    `json:"days,omitempty"`
	State        string `json:"state,omitempty"`
	TestAccount  bool   `json:"test_account,omitempty"`
}

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
	TestAccount               bool       `json:"test_account"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

type AdminStats struct {
	TotalUsers   int `json:"total_users"`
	ActiveUsers  int `json:"active_users"`
	PremiumUsers int `json:"premium_users"`
	VIPUsers     int `json:"vip_users"`
	PaidUsers    int `json:"paid_users"`
}

type AdminEnrollmentDay struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type AdminEnrollmentView struct {
	Days  []AdminEnrollmentDay `json:"days"`
	Stats AdminStats           `json:"stats"`
}

type AdminTokenView struct {
	Present bool `json:"present"`
}

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
	rpc.Refusal
}

type AuthRequest struct {
	ActorID string `json:"actor_id"`
	// ActorRole is client-supplied; never authorize on it.
	ActorRole string `json:"actor_role"`

	UserID      string `json:"user_id"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`

	ActorLogin string `json:"actor_login"`
	Action     string `json:"action"`
	Target     string `json:"target"`
	Detail     string `json:"detail"`
	OK         bool   `json:"ok"`
	Err        string `json:"error"`

	Limit       int    `json:"limit"`
	ActorFilter string `json:"actor_filter"`
	Page        int    `json:"page"`
	Search      string `json:"search"`
}

type AdminAcctView struct {
	ID          uint64    `json:"id"`
	Login       string    `json:"login"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	Active      bool      `json:"active"`
	AddedBy     uint64    `json:"added_by"`
	CreatedAt   time.Time `json:"created_at"`
}

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
	rpc.Refusal
}

type UpsertUserRequest struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email,omitempty"`
}

type LoginResolveRequest struct {
	Login string `json:"login"`
}

type LoginResolveReply struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	rpc.Refusal
}

type EmailGetRequest struct {
	UserID string `json:"user_id"`
}

type EmailGetReply struct {
	Email  string `json:"email,omitempty"`
	Locale string `json:"locale,omitempty"`
	rpc.Refusal
}

type CountsRequest struct{}

type CountsReply struct {
	TotalUsers  int `json:"total_users"`
	ActiveUsers int `json:"active_users"`
	rpc.Refusal
}

type GrantSaveRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	AccessToken       string `json:"access_token"`
	RefreshToken      string `json:"refresh_token"`
}

type GrantHasRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

type ActiveSetRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Active            bool   `json:"active"`
}

type DeleteSelfRequest struct {
	UserID string `json:"user_id"`
}

type LocaleSetRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Locale            string `json:"locale"`
}

type StateGetRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

type StateGetReply struct {
	Active    bool   `json:"active"`
	Status    string `json:"status"`
	Onboarded bool   `json:"onboarded"`
	Locale    string `json:"locale"`
	rpc.Refusal
}

type OnboardedSetRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Onboarded         bool   `json:"onboarded"`
}

type CursorSetRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	CustomCursor      bool   `json:"custom_cursor"`
}

type CommandsPageSetRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Hidden            bool   `json:"commands_page_hidden"`
}

type CreateDelegationRequest struct {
	OwnerUserID string   `json:"owner_user_id"`
	OwnerLogin  string   `json:"owner_login"`
	Sections    []string `json:"sections"`
}

type TokenRequest struct {
	Token string `json:"token"`
}

type ConsumeDelegationRequest struct {
	Token          string `json:"token"`
	DelegateUserID string `json:"delegate_user_id"`
	DelegateLogin  string `json:"delegate_login"`
}

type OwnerRequest struct {
	OwnerUserID string `json:"owner_user_id"`
}

type RevokeDelegationRequest struct {
	OwnerUserID string `json:"owner_user_id"`
	Token       string `json:"token"`
}

type UpdateDelegationRequest struct {
	OwnerUserID string   `json:"owner_user_id"`
	Token       string   `json:"token"`
	Sections    []string `json:"sections"`
}

type AccessRequest struct {
	DelegateUserID string `json:"delegate_user_id"`
}

type OptOutDelegationRequest struct {
	OwnerUserID    string `json:"owner_user_id"`
	DelegateUserID string `json:"delegate_user_id"`
}

type TokensRequest struct {
	UserID               string     `json:"user_id"`
	AccessToken          string     `json:"access_token"`
	RefreshToken         string     `json:"refresh_token"`
	AccessTokenExpiresAt *time.Time `json:"access_token_expires_at,omitempty"`
}

type TokensReply struct {
	AccessToken          string     `json:"access_token,omitempty"`
	RefreshToken         string     `json:"refresh_token,omitempty"`
	AccessTokenExpiresAt *time.Time `json:"access_token_expires_at,omitempty"`
	rpc.Refusal
}
