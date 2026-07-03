package rpc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/notifications/repository"
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

// UserConfig carries the NATS wiring for the dashboard-facing RPC surface.
type UserConfig struct {
	Prefix     string
	QueueGroup string
	// FullReadTTL is how long a fully-read notification lingers for a user
	// before it drops out of their list; PeekTTL is the (longer) reduced life a
	// dropdown peek grants an as-yet-unread one.
	FullReadTTL time.Duration
	PeekTTL     time.Duration
}

// SubscribeUser registers the dashboard-facing verbs: list what a user can
// see (broadcast + direct, newest first), fully read one, and soft-acknowledge
// (peek) all of them when the bell dropdown opens.
func SubscribeUser(nc *nats.Conn, repo *repository.Notifications, cfg UserConfig, app *newrelic.Application, log *zap.Logger) error {
	u := &userRPC{repo: repo, fullReadTTL: cfg.FullReadTTL, peekTTL: cfg.PeekTTL, log: log}

	if err := bus.QueueSubscribeJSON[notificationsrpc.UserListRequest, notificationsrpc.UserListReply](
		nc, cfg.Prefix+".list", cfg.QueueGroup, 3*time.Second, app, log, u.list); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[notificationsrpc.MarkReadRequest, notificationsrpc.MarkReadReply](
		nc, cfg.Prefix+".mark_read", cfg.QueueGroup, 3*time.Second, app, log, u.markRead); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[notificationsrpc.MarkPeekedRequest, notificationsrpc.MarkPeekedReply](
		nc, cfg.Prefix+".mark_peeked", cfg.QueueGroup, 3*time.Second, app, log, u.markPeeked); err != nil {
		return err
	}
	return nil
}

func (u *userRPC) list(ctx context.Context, req notificationsrpc.UserListRequest) notificationsrpc.UserListReply {
	userID, err := parseUserID(req.UserID)
	if err != nil {
		return notificationsrpc.UserListReply{Error: err.Error()}
	}

	rows, read, err := u.repo.ListForUser(ctx, userID, repository.UserListLimit)
	if err != nil {
		return notificationsrpc.UserListReply{Error: err.Error()}
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
		return notificationsrpc.MarkReadReply{Error: err.Error()}
	}
	notifID, err := strconv.Atoi(req.NotificationID)
	if err != nil {
		return notificationsrpc.MarkReadReply{Error: "notification_id must be numeric"}
	}
	if err := u.repo.MarkRead(ctx, notifID, userID, time.Now().Add(u.fullReadTTL)); err != nil {
		return notificationsrpc.MarkReadReply{Error: err.Error()}
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
		return notificationsrpc.MarkPeekedReply{Error: err.Error()}
	}
	peeked, err := u.repo.MarkPeeked(ctx, userID, time.Now().Add(u.peekTTL))
	if err != nil {
		return notificationsrpc.MarkPeekedReply{Error: err.Error()}
	}
	return notificationsrpc.MarkPeekedReply{Peeked: peeked}
}

func parseUserID(s string) (uint64, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("user_id must be numeric")
	}
	return id, nil
}
