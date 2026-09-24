// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package notificationsrpc

import "ItsBagelBot/internal/domain/rpc"

import "time"

type NotificationView struct {
	ID             int64      `json:"id"`
	Scope          string     `json:"scope"`
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	Level          string     `json:"level"`
	TargetUserID   *uint64    `json:"target_user_id,omitempty"`
	CreatedByLogin string     `json:"created_by_login"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Read           bool       `json:"read"`
}

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
	rpc.Refusal
}

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
	rpc.Refusal
}

type DeleteRequest struct {
	ID int64 `json:"id"`
}

type DeleteReply struct {
	rpc.Refusal
}

type UserListRequest struct {
	UserID string `json:"user_id"`
}

type UserListReply struct {
	Notifications []NotificationView `json:"notifications,omitempty"`
	UnreadCount   int                `json:"unread_count"`
	rpc.Refusal
}

type MarkReadRequest struct {
	UserID         string `json:"user_id"`
	NotificationID string `json:"notification_id"`
}

type MarkReadReply struct {
	rpc.Refusal
}

type MarkPeekedRequest struct {
	UserID string `json:"user_id"`
}

type MarkPeekedReply struct {
	Peeked int `json:"peeked"`
	rpc.Refusal
}

type CleanupRequest struct{}

type CleanupReply struct {
	Deleted int `json:"deleted"`
	rpc.Refusal
}
