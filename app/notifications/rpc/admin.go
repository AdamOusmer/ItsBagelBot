package rpc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/notifications/ent/notification"
	"ItsBagelBot/app/notifications/repository"
	"ItsBagelBot/internal/domain/invalidate"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

type adminRPC struct {
	repo               *repository.Notifications
	nc                 *nats.Conn
	invalidationPrefix string
	userGetSubject     string
	// defaultTTL bounds a notification's global life when the sender does not
	// set an explicit expiry, so every send is eventually reachable by the
	// cron janitor instead of living forever.
	defaultTTL time.Duration
	log        *zap.Logger
}

// AdminConfig carries the NATS wiring for the admin RPC surface.
type AdminConfig struct {
	Prefix             string
	InvalidationPrefix string
	UserGetSubject     string
	QueueGroup         string
	// DefaultTTL bounds a notification's global life when the sender does not
	// set an explicit expiry, so every send is eventually reachable by the
	// cron janitor instead of living forever.
	DefaultTTL time.Duration
}

// SubscribeAdmin registers the admin-console verbs: compose/send a
// notification, list the sent history, and retract one.
func SubscribeAdmin(nc *nats.Conn, repo *repository.Notifications, cfg AdminConfig, app *newrelic.Application, log *zap.Logger) error {
	a := &adminRPC{
		repo:               repo,
		nc:                 nc,
		invalidationPrefix: cfg.InvalidationPrefix,
		userGetSubject:     cfg.UserGetSubject,
		defaultTTL:         cfg.DefaultTTL,
		log:                log,
	}

	if err := bus.QueueSubscribeJSON[notificationsrpc.SendRequest, notificationsrpc.SendReply](
		a.nc, cfg.Prefix+".send", cfg.QueueGroup, 5*time.Second, app, log, a.send); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[notificationsrpc.ListAdminRequest, notificationsrpc.ListAdminReply](
		a.nc, cfg.Prefix+".list", cfg.QueueGroup, 3*time.Second, app, log, a.list); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[notificationsrpc.DeleteRequest, notificationsrpc.DeleteReply](
		a.nc, cfg.Prefix+".delete", cfg.QueueGroup, 3*time.Second, app, log, a.delete); err != nil {
		return err
	}
	return nil
}

// parseSendRequest validates the compose form fields and renders them as
// repository parameters (target resolution happens separately: it needs I/O).
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
	params, err := parseSendRequest(req)
	if err != nil {
		return notificationsrpc.SendReply{Error: err.Error()}
	}

	if params.Scope == notification.ScopeDirect {
		id, err := a.resolveTarget(ctx, req.TargetUserID, req.TargetUsername)
		if err != nil {
			return notificationsrpc.SendReply{Error: err.Error()}
		}
		params.TargetUserID = &id
	}

	// Fall back to the default global TTL when the sender didn't pin an expiry,
	// so the notification is eventually swept by the cron instead of living
	// forever.
	if params.ExpiresAt == nil && a.defaultTTL > 0 {
		exp := time.Now().Add(a.defaultTTL)
		params.ExpiresAt = &exp
	}

	row, created, err := a.repo.Create(ctx, params)
	if err != nil {
		return notificationsrpc.SendReply{Error: err.Error()}
	}

	if created {
		a.invalidate(params.TargetUserID)
		a.log.Info("admin notification sent",
			zap.String("scope", req.Scope), zap.Int("id", row.ID), zap.Uint64("actor", params.CreatedBy))
	} else {
		a.log.Warn("duplicate admin notification suppressed",
			zap.String("scope", req.Scope), zap.Int("id", row.ID), zap.Uint64("actor", params.CreatedBy))
	}

	view := viewOf(row, false)
	return notificationsrpc.SendReply{Notification: &view}
}

// clampAdminPage bounds the requested page/limit to the admin window and
// returns the fetch limit (one extra row probes for a following page).
func clampAdminPage(req notificationsrpc.ListAdminRequest) (page, pageSize, fetchLimit int) {
	pageSize = req.Limit
	if pageSize <= 0 || pageSize > repository.AdminPageSize {
		pageSize = repository.AdminPageSize
	}
	page = req.Page
	if page < 1 {
		page = 1
	}
	if page > repository.AdminMaxPages {
		page = repository.AdminMaxPages
	}
	fetchLimit = pageSize
	if page < repository.AdminMaxPages {
		fetchLimit++
	}
	return page, pageSize, fetchLimit
}

func (a *adminRPC) list(ctx context.Context, req notificationsrpc.ListAdminRequest) notificationsrpc.ListAdminReply {
	page, pageSize, fetchLimit := clampAdminPage(req)
	rows, err := a.repo.ListForAdmin(ctx, fetchLimit, (page-1)*pageSize)
	if err != nil {
		return notificationsrpc.ListAdminReply{Error: err.Error()}
	}
	hasMore := page < repository.AdminMaxPages && len(rows) > pageSize
	if hasMore {
		rows = rows[:pageSize]
	}

	views := make([]notificationsrpc.NotificationView, 0, len(rows))
	for _, row := range rows {
		views = append(views, viewOf(row, false))
	}

	return notificationsrpc.ListAdminReply{
		Notifications: views,
		Page:          page,
		PageSize:      pageSize,
		MaxPages:      repository.AdminMaxPages,
		HasMore:       hasMore,
	}
}

func (a *adminRPC) delete(ctx context.Context, req notificationsrpc.DeleteRequest) notificationsrpc.DeleteReply {
	if req.ID <= 0 {
		return notificationsrpc.DeleteReply{Error: "id required"}
	}
	if err := a.repo.Delete(ctx, int(req.ID)); err != nil {
		return notificationsrpc.DeleteReply{Error: err.Error()}
	}
	a.invalidate(nil)
	return notificationsrpc.DeleteReply{}
}

// resolveTarget looks up the target's numeric id via the users service admin
// surface (bagel.rpc.admin.user.get) so the console can address a direct
// notification by username as well as by id.
func (a *adminRPC) resolveTarget(ctx context.Context, userID, username string) (uint64, error) {
	if userID == "" && username == "" {
		return 0, fmt.Errorf("target_user_id or target_username required")
	}
	reply, err := bus.RequestJSONTimeout[usersrpc.AdminReply](ctx, a.nc, a.userGetSubject,
		usersrpc.AdminRequest{UserID: userID, Username: username}, 3*time.Second)
	if err != nil {
		return 0, fmt.Errorf("resolve target user: %w", err)
	}
	if reply.Error != "" {
		return 0, fmt.Errorf("resolve target user: %s", reply.Error)
	}
	if reply.User == nil {
		return 0, fmt.Errorf("target user not found")
	}
	return reply.User.ID, nil
}

func (a *adminRPC) invalidate(targetUserID *uint64) {
	id := "*"
	if targetUserID != nil {
		id = fmt.Sprint(*targetUserID)
	}
	if err := invalidate.Publish(a.nc, a.invalidationPrefix, "notifications", id); err != nil {
		a.log.Warn("cache invalidation publish failed", zap.Error(err))
	}
}
