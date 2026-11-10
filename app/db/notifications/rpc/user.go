// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/db/notifications/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env/conf"
)

type userRPC struct {
	Wiring
	userGetSubject     string
	invalidationPrefix string
	fullReadTTL        time.Duration
	peekTTL            time.Duration
}

type UserConfig struct {
	UserGetSubject     string
	Prefix             string
	InvalidationPrefix string
	FullReadTTL        time.Duration
	PeekTTL            time.Duration
}

func SubscribeUser(w Wiring, cfg UserConfig) error {
	if cfg.UserGetSubject == "" {
		cfg.UserGetSubject = conf.UsersGetSubject.Get()
	}
	u := &userRPC{Wiring: w, userGetSubject: cfg.UserGetSubject, invalidationPrefix: cfg.InvalidationPrefix, fullReadTTL: cfg.FullReadTTL, peekTTL: cfg.PeekTTL}

	read := w.Within(readBudget)
	if err := bus.ServeForUser[notificationsrpc.UserListRequest, notificationsrpc.UserListReply](read, cfg.Prefix+".list", u.list); err != nil {
		return err
	}
	if err := bus.ServeForUser[notificationsrpc.MarkReadRequest, notificationsrpc.MarkReadReply](read, cfg.Prefix+".mark_read", u.markRead); err != nil {
		return err
	}
	return bus.ServeForUser[notificationsrpc.MarkPeekedRequest, notificationsrpc.MarkPeekedReply](read, cfg.Prefix+".mark_peeked", u.markPeeked)
}

func (u *userRPC) recipient(ctx context.Context, userID string) (repository.Recipient, error) {
	return notificationAccount(ctx, u.NC, u.userGetSubject, usersrpc.AdminRequest{UserID: userID})
}

func (u *userRPC) list(ctx context.Context, req notificationsrpc.UserListRequest, _ uint64) (notificationsrpc.UserListReply, error) {
	recipient, err := u.recipient(ctx, req.UserID)
	if err != nil {
		return notificationsrpc.UserListReply{}, err
	}
	rows, read, err := u.Repo.ListForIncarnation(ctx, recipient, repository.UserListLimit)
	if err != nil {
		return notificationsrpc.UserListReply{}, err
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

	return notificationsrpc.UserListReply{Notifications: views, UnreadCount: unread}, nil
}

func (u *userRPC) markRead(ctx context.Context, req notificationsrpc.MarkReadRequest, userID uint64) (notificationsrpc.MarkReadReply, error) {
	notifID, err := strconv.Atoi(req.NotificationID)
	if err != nil {
		return notificationsrpc.MarkReadReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "notification_id must be numeric")}, nil
	}
	recipient, err := u.recipient(ctx, req.UserID)
	if err != nil {
		return notificationsrpc.MarkReadReply{}, err
	}
	if err := u.Repo.MarkReadForIncarnation(ctx, notifID, recipient, time.Now().Add(u.fullReadTTL)); err != nil {
		return notificationsrpc.MarkReadReply{}, err
	}
	u.invalidateNotifications(u.invalidationPrefix, accountKey(userID))
	return notificationsrpc.MarkReadReply{}, nil
}

func (u *userRPC) markPeeked(ctx context.Context, req notificationsrpc.MarkPeekedRequest, userID uint64) (notificationsrpc.MarkPeekedReply, error) {
	recipient, err := u.recipient(ctx, req.UserID)
	if err != nil {
		return notificationsrpc.MarkPeekedReply{}, err
	}
	peeked, err := u.Repo.MarkPeekedForIncarnation(ctx, recipient, time.Now().Add(u.peekTTL))
	if err != nil {
		return notificationsrpc.MarkPeekedReply{}, err
	}
	if peeked > 0 {
		u.invalidateNotifications(u.invalidationPrefix, accountKey(userID))
	}
	return notificationsrpc.MarkPeekedReply{Peeked: peeked}, nil
}
