// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/notifications/ent/notification"
	"ItsBagelBot/app/db/notifications/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"
)

type adminRPC struct {
	Wiring
	invalidationPrefix string
	userGetSubject     string
	defaultTTL         time.Duration
}

type AdminConfig struct {
	Prefix             string
	InvalidationPrefix string
	UserGetSubject     string
	DefaultTTL         time.Duration
}

func SubscribeAdmin(w Wiring, cfg AdminConfig) error {
	a := &adminRPC{
		Wiring:             w,
		invalidationPrefix: cfg.InvalidationPrefix,
		userGetSubject:     cfg.UserGetSubject,
		defaultTTL:         cfg.DefaultTTL,
	}

	read := w.Within(readBudget)
	if err := bus.Serve(w.Within(sendBudget), cfg.Prefix+".send", a.send); err != nil {
		return err
	}
	if err := bus.Serve(read, cfg.Prefix+".list", a.list); err != nil {
		return err
	}
	return bus.Serve(read, cfg.Prefix+".delete", a.delete)
}

func parseSendRequest(req notificationsrpc.SendRequest) (repository.CreateParams, error) {
	scope := notification.Scope(req.Scope)
	if err := notification.ScopeValidator(scope); err != nil {
		return repository.CreateParams{}, fmt.Errorf("scope must be broadcast or direct")
	}
	if req.Title == "" || req.Body == "" {
		return repository.CreateParams{}, fmt.Errorf("title and body are required")
	}

	level := notification.Level(req.Level)
	if level == "" {
		level = notification.LevelInfo
	}
	if err := notification.LevelValidator(level); err != nil {
		return repository.CreateParams{}, fmt.Errorf("level must be info, success, warning or critical")
	}
	actorID, err := strconv.ParseUint(req.ActorID, 10, 64)
	if err != nil {
		return repository.CreateParams{}, fmt.Errorf("actor_id must be numeric")
	}

	return repository.CreateParams{
		RequestID:      req.RequestID,
		Scope:          scope,
		Title:          req.Title,
		Body:           req.Body,
		Level:          level,
		CreatedBy:      actorID,
		CreatedByLogin: req.ActorLogin,
		ExpiresAt:      req.ExpiresAt,
	}, nil
}

func (a *adminRPC) send(ctx context.Context, req notificationsrpc.SendRequest) notificationsrpc.SendReply {
	log := monitor.TxnLogger(ctx, a.Log)
	params, err := parseSendRequest(req)
	if err != nil {
		return notificationsrpc.SendReply{Refusal: bus.Classify(err)}
	}

	if params.Scope == notification.ScopeDirect {
		recipient, err := notificationAccount(ctx, a.NC, a.userGetSubject, usersrpc.AdminRequest{UserID: req.TargetUserID, Username: req.TargetUsername})
		if err != nil {
			return notificationsrpc.SendReply{Refusal: bus.Classify(err)}
		}
		params.TargetUserID = &recipient.UserID
		params.Incarnation = recipient.Incarnation
	}

	if params.ExpiresAt == nil && a.defaultTTL > 0 {
		exp := time.Now().Add(a.defaultTTL)
		params.ExpiresAt = &exp
	}

	row, created, err := a.Repo.Create(ctx, params)
	if err != nil {
		return notificationsrpc.SendReply{Refusal: bus.Classify(err)}
	}

	if created {
		a.invalidate(params.TargetUserID)
		log.Info("admin notification sent",
			zap.String("scope", req.Scope), zap.Int("id", row.ID), zap.Uint64("actor", params.CreatedBy))
	} else {
		log.Warn("duplicate admin notification suppressed",
			zap.String("scope", req.Scope), zap.Int("id", row.ID), zap.Uint64("actor", params.CreatedBy))
	}

	view := viewOf(row, false)
	return notificationsrpc.SendReply{Notification: &view}
}

func (a *adminRPC) list(ctx context.Context, req notificationsrpc.ListAdminRequest) notificationsrpc.ListAdminReply {
	size := req.Limit
	if size <= 0 {
		size = repository.AdminPageSize
	}
	page := domainrpc.ClampPage(req.Page, size, repository.AdminPageSize, repository.AdminMaxPages)
	rows, err := a.Repo.ListForAdmin(ctx, page.FetchLimit(), page.Offset())
	if err != nil {
		return notificationsrpc.ListAdminReply{Refusal: bus.Classify(err)}
	}
	rows, hasMore := domainrpc.PageRows(page, rows)

	views := make([]notificationsrpc.NotificationView, 0, len(rows))
	for _, row := range rows {
		views = append(views, viewOf(row, false))
	}

	return notificationsrpc.ListAdminReply{
		Notifications: views,
		Page:          page.Number,
		PageSize:      page.Size,
		MaxPages:      repository.AdminMaxPages,
		HasMore:       hasMore,
	}
}

func (a *adminRPC) delete(ctx context.Context, req notificationsrpc.DeleteRequest) notificationsrpc.DeleteReply {
	if req.ID <= 0 {
		return notificationsrpc.DeleteReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "id required")}
	}
	if err := a.Repo.Delete(ctx, int(req.ID)); err != nil {
		return notificationsrpc.DeleteReply{Refusal: bus.Classify(err)}
	}
	a.invalidate(nil)
	return notificationsrpc.DeleteReply{}
}

func (a *adminRPC) invalidate(targetUserID *uint64) {
	id := "*"
	if targetUserID != nil {
		id = accountKey(*targetUserID)
	}
	a.invalidateNotifications(a.invalidationPrefix, id)
}
