// Package notificationsrpc holds the shared wire types for the notifications
// service RPC surface, transcribed verbatim from app/notifications/rpc so
// consumers can reference them without pulling in the full service.
package notificationsrpc

import "time"

// NotificationView is a single notification row on the wire.
type NotificationView struct {
	ID             int64      `json:"id"`
	Scope          string     `json:"scope"` // "broadcast" | "direct"
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	Level          string     `json:"level"` // "info" | "success" | "warning" | "critical"
	TargetUserID   *uint64    `json:"target_user_id,omitempty"`
	CreatedByLogin string     `json:"created_by_login"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Read           bool       `json:"read"`
}

// SendRequest is the payload for the admin send verb. Exactly one of
// TargetUserID / TargetUsername should be set for scope=direct; both are
// ignored for scope=broadcast.
type SendRequest struct {
	Scope          string     `json:"scope"`
	TargetUserID   string     `json:"target_user_id,omitempty"`
	TargetUsername string     `json:"target_username,omitempty"`
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	Level          string     `json:"level"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	ActorID        string     `json:"actor_id"`
	ActorLogin     string     `json:"actor_login"`
	RequestID      string     `json:"request_id,omitempty"`
}

type SendReply struct {
	Notification *NotificationView `json:"notification,omitempty"`
	Error        string            `json:"error,omitempty"`
}

// ListAdminRequest pages through every notification for the admin console.
type ListAdminRequest struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

type ListAdminReply struct {
	Notifications []NotificationView `json:"notifications,omitempty"`
	Page          int                `json:"page,omitempty"`
	PageSize      int                `json:"page_size,omitempty"`
	MaxPages      int                `json:"max_pages,omitempty"`
	HasMore       bool               `json:"has_more,omitempty"`
	Error         string             `json:"error,omitempty"`
}

type DeleteRequest struct {
	ID int64 `json:"id"`
}

type DeleteReply struct {
	Error string `json:"error,omitempty"`
}

// UserListRequest is the payload for the user-facing list verb.
type UserListRequest struct {
	UserID string `json:"user_id"`
}

type UserListReply struct {
	Notifications []NotificationView `json:"notifications,omitempty"`
	UnreadCount   int                `json:"unread_count"`
	Error         string             `json:"error,omitempty"`
}

// MarkReadRequest is the payload for the user-facing mark_read verb.
type MarkReadRequest struct {
	UserID         string `json:"user_id"`
	NotificationID string `json:"notification_id"`
}

type MarkReadReply struct {
	Error string `json:"error,omitempty"`
}

// MarkPeekedRequest is the payload for the user-facing mark_peeked verb, fired
// when the topbar bell dropdown is opened. It soft-acknowledges every
// notification the user can currently see, shortening each one's per-user life
// to the reduced peek TTL (a full read shortens it further still).
type MarkPeekedRequest struct {
	UserID string `json:"user_id"`
}

type MarkPeekedReply struct {
	// Peeked is how many previously-unacknowledged notifications this call
	// newly marked; 0 means everything was already read or peeked.
	Peeked int    `json:"peeked"`
	Error  string `json:"error,omitempty"`
}

// CleanupRequest is the payload for the internal maintenance cleanup verb the
// k3s cron drives. It carries no arguments; the service sweeps whatever is
// globally expired at handling time.
type CleanupRequest struct{}

type CleanupReply struct {
	// Deleted is the number of globally-expired notifications swept (their read
	// receipts cascade).
	Deleted int    `json:"deleted"`
	Error   string `json:"error,omitempty"`
}
