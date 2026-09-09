// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strconv"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/notifications/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	"ItsBagelBot/pkg/bus"
)

type userRPC struct {
	repo *repository.Notifications
	// fullReadTTL is how long a fully-read notification lingers for a user
	// before it drops out of their list; peekTTL is the (longer) reduced life a
	// dropdown peek grants an as-yet-unread one.
	fullReadTTL time.Duration
	peekTTL     time.Duration
	log         *zap.Logger
}

// UserConfig carries the subject prefix and the TTL tiers for the
// dashboard-facing RPC surface. The connection, queue group, New Relic app and
// logger ride the shared Wiring instead.
type UserConfig struct {
	Prefix string
	// FullReadTTL is how long a fully-read notification lingers for a user
	// before it drops out of their list; PeekTTL is the (longer) reduced life a
	// dropdown peek grants an as-yet-unread one.
	FullReadTTL time.Duration
	PeekTTL     time.Duration
}

// SubscribeUser registers the dashboard-facing verbs: list what a user can
// see (broadcast + direct, newest first), fully read one, and soft-acknowledge
// (peek) all of them when the bell dropdown opens.
func SubscribeUser(w Wiring, cfg UserConfig) error {
	u := &userRPC{repo: w.Repo, fullReadTTL: cfg.FullReadTTL, peekTTL: cfg.PeekTTL, log: w.Log}

	read := w.Within(readBudget)
	if err := bus.Serve(read, cfg.Prefix+".list", u.list); err != nil {
		return err
	}
	if err := bus.Serve(read, cfg.Prefix+".mark_read", u.markRead); err != nil {
		return err
	}
	return bus.Serve(read, cfg.Prefix+".mark_peeked", u.markPeeked)
}

func (u *userRPC) list(ctx context.Context, req notificationsrpc.UserListRequest) notificationsrpc.UserListReply {
	userID, err := parseUserID(req.UserID)
	if err != nil {
		return notificationsrpc.UserListReply{Refusal: bus.Classify(err)}
	}

	rows, read, err := u.repo.ListForUser(ctx, userID, repository.UserListLimit)
	if err != nil {
		return notificationsrpc.UserListReply{Refusal: bus.Classify(err)}
	}

	views := make([]notificationsrpc.NotificationView, 0, len(rows))
	unread := 0
	for _, row := range rows {
		isRead := read[row.ID]
		if !isRead {
			unread++
		}
		views = append(views, viewOf(row, isRead))
	}

	return notificationsrpc.UserListReply{Notifications: views, UnreadCount: unread}
}

func (u *userRPC) markRead(ctx context.Context, req notificationsrpc.MarkReadRequest) notificationsrpc.MarkReadReply {
	userID, err := parseUserID(req.UserID)
	if err != nil {
		return notificationsrpc.MarkReadReply{Refusal: bus.Classify(err)}
	}
	notifID, err := strconv.Atoi(req.NotificationID)
	if err != nil {
		return notificationsrpc.MarkReadReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "notification_id must be numeric")}
	}
	if err := u.repo.MarkRead(ctx, notifID, userID, time.Now().Add(u.fullReadTTL)); err != nil {
		return notificationsrpc.MarkReadReply{Refusal: bus.Classify(err)}
	}
	return notificationsrpc.MarkReadReply{}
}

// markPeeked soft-acknowledges every notification the user can currently see:
// opening the bell dropdown counts as "seen", so unread items get the reduced
// peek cutoff and the badge clears, while a later full read can still shorten an
// item's life further.
func (u *userRPC) markPeeked(ctx context.Context, req notificationsrpc.MarkPeekedRequest) notificationsrpc.MarkPeekedReply {
	userID, err := parseUserID(req.UserID)
	if err != nil {
		return notificationsrpc.MarkPeekedReply{Refusal: bus.Classify(err)}
	}
	peeked, err := u.repo.MarkPeeked(ctx, userID, time.Now().Add(u.peekTTL))
	if err != nil {
		return notificationsrpc.MarkPeekedReply{Refusal: bus.Classify(err)}
	}
	return notificationsrpc.MarkPeekedReply{Peeked: peeked}
}

// parseUserID is bus.UserID under this package's name: the admin send verb
// resolves a recipient partway through its own validation, so the guard cannot
// be the bind-time bus.ServeForUser prologue here.
func parseUserID(s string) (uint64, error) { return bus.UserID(s) }
