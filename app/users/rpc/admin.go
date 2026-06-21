package rpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/tokens"
	"ItsBagelBot/app/users/ent/user"
	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/internal/domain/invalidate"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

type adminRPC struct {
	repo               *repository.Users
	nc                 *nats.Conn
	invalidationPrefix string
	log                *zap.Logger
}

const (
	adminUserPageSize     = repository.AdminUserPageSize
	adminUserMaxPages     = repository.AdminUserMaxPages
	adminUserMaxSearchLen = repository.AdminUserMaxSearchLen
)

func SubscribeAdmin(nc *nats.Conn, repo *repository.Users, prefix, invalidationPrefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	a := &adminRPC{
		repo:               repo,
		nc:                 nc,
		invalidationPrefix: invalidationPrefix,
		log:                log,
	}

	verbs := map[string]func(context.Context, usersrpc.AdminRequest) usersrpc.AdminReply{
		"get":          a.get,
		"list":         a.list,
		"stats":        a.stats,
		"overview":     a.overview,
		"set_status":   a.setStatus,
		"set_active":   a.setActive,
		"ban":          a.ban,
		"unban":        a.unban,
		"reset":        a.reset,
		"token_set":    a.tokenSet,
		"token_status": a.tokenStatus,
		"token_clear":  a.tokenClear,
		"delete":       a.delete,
	}
	for verb, handle := range verbs {
		subject := prefix + "." + verb
		if err := bus.QueueSubscribeJSON[usersrpc.AdminRequest, usersrpc.AdminReply](a.nc, subject, queueGroup, 3*time.Second, app, log, handle); err != nil {
			return err
		}
	}
	return nil
}

func (a *adminRPC) get(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}
	view := viewOf(u)
	return usersrpc.AdminReply{User: &view}
}

func (a *adminRPC) list(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if req.Page > 0 {
		page := req.Page
		if page < 1 {
			page = 1
		}
		if page > adminUserMaxPages {
			page = adminUserMaxPages
		}
		pageSize := limit
		if pageSize <= 0 || pageSize > adminUserPageSize {
			pageSize = adminUserPageSize
		}
		fetchLimit := pageSize
		if page < adminUserMaxPages {
			fetchLimit++
		}
		rows, err := a.repo.ListUsers(ctx, req.Search, fetchLimit, (page-1)*pageSize)
		if err != nil {
			return usersrpc.AdminReply{Error: err.Error()}
		}
		hasMore := page < adminUserMaxPages && len(rows) > pageSize
		if hasMore {
			rows = rows[:pageSize]
		}
		return usersrpc.AdminReply{
			Users:    userViewsOf(rows),
			Page:     page,
			PageSize: pageSize,
			MaxPages: adminUserMaxPages,
			HasMore:  hasMore,
		}
	}
	rows, err := a.repo.ListUsers(ctx, req.Search, limit, 0)
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}
	return usersrpc.AdminReply{Users: userViewsOf(rows)}
}

func (a *adminRPC) overview(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	list := a.list(ctx, req)
	if list.Error != "" {
		return list
	}
	stats := a.stats(ctx, req)
	if stats.Error != "" {
		return stats
	}
	list.Stats = stats.Stats
	return list
}

func (a *adminRPC) stats(ctx context.Context, _ usersrpc.AdminRequest) usersrpc.AdminReply {
	total, active, paid, vip, err := a.repo.UserStats(ctx)
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}
	stats := usersrpc.AdminStats{
		TotalUsers:   total,
		ActiveUsers:  active,
		PremiumUsers: paid + vip,
		VIPUsers:     vip,
		PaidUsers:    paid,
	}
	return usersrpc.AdminReply{Stats: &stats}
}

func (a *adminRPC) setStatus(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if errors.Is(err, repository.ErrUserNotFound) && req.UserID != "" {
		u, err = a.provision(ctx, req.UserID)
	}
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	status := user.Status(req.Status)
	if err := user.StatusValidator(status); err != nil {
		return usersrpc.AdminReply{Error: "status must be free, paid or vip"}
	}

	if err := a.repo.SetStatus(ctx, u.ID, status); err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin status change",
		zap.Uint64("user", u.ID), zap.String("status", req.Status))
	return a.get(ctx, usersrpc.AdminRequest{UserID: fmt.Sprint(u.ID)})
}

// setActive flips whether the bot serves this broadcaster. Inactive users
// project to standard tier and the ingress drops their traffic.
func (a *adminRPC) setActive(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	if err := a.repo.SetActive(ctx, u.ID, req.Active); err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin set active", zap.Uint64("user", u.ID), zap.Bool("active", req.Active))
	return a.get(ctx, usersrpc.AdminRequest{UserID: fmt.Sprint(u.ID)})
}

// ban blocks the user from the service entirely. The ingress drops banned
// users, so their traffic never reaches a worker.
func (a *adminRPC) ban(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	if err := a.repo.SetBanned(ctx, u.ID, true); err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin ban", zap.Uint64("user", u.ID))
	return a.get(ctx, usersrpc.AdminRequest{UserID: fmt.Sprint(u.ID)})
}

// unban lifts a previous ban, allowing the user's traffic through again.
func (a *adminRPC) unban(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	if err := a.repo.SetBanned(ctx, u.ID, false); err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin unban", zap.Uint64("user", u.ID))
	return a.get(ctx, usersrpc.AdminRequest{UserID: fmt.Sprint(u.ID)})
}

// reset clears all tokens for the user. Configs and timers are managed
// externally, so token wipe is the scope of this operation.
func (a *adminRPC) reset(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	if err := a.repo.ResetTokens(ctx, u.ID); err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin state reset", zap.Uint64("user", u.ID))
	return a.get(ctx, usersrpc.AdminRequest{UserID: fmt.Sprint(u.ID)})
}

// tokenSet stores (or replaces) the user's Twitch OAuth token. This is how
// the operator installs the bot account's own token; the row is provisioned
// on first sight so the bot account does not need to onboard like a
// broadcaster.
func (a *adminRPC) tokenSet(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if errors.Is(err, repository.ErrUserNotFound) && req.UserID != "" {
		u, err = a.provision(ctx, req.UserID)
	}
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	if err := a.repo.UpsertToken(ctx, u.ID, tokens.TypeUserToken, tokens.PlatformTwitch,
		[]byte(req.AccessToken), []byte(req.RefreshToken)); err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin token set", zap.Uint64("user", u.ID))
	return a.tokenStatus(ctx, usersrpc.AdminRequest{UserID: fmt.Sprint(u.ID)})
}

func (a *adminRPC) tokenStatus(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	present, err := a.repo.HasToken(ctx, u.ID, tokens.TypeUserToken, tokens.PlatformTwitch)
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	return usersrpc.AdminReply{Token: &usersrpc.AdminTokenView{Present: present}}
}

func (a *adminRPC) tokenClear(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	if err := a.repo.ClearToken(ctx, u.ID, tokens.TypeUserToken, tokens.PlatformTwitch); err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin token cleared", zap.Uint64("user", u.ID))
	return a.tokenStatus(ctx, usersrpc.AdminRequest{UserID: fmt.Sprint(u.ID)})
}

func (a *adminRPC) delete(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	if err := a.repo.Delete(ctx, u.ID); err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	a.log.Info("admin user delete", zap.Uint64("user", u.ID))
	return usersrpc.AdminReply{}
}

func (a *adminRPC) provision(ctx context.Context, userID string) (*ent.User, error) {
	var id uint64
	if _, err := fmt.Sscanf(userID, "%d", &id); err != nil {
		return nil, fmt.Errorf("user_id must be numeric")
	}

	email := fmt.Sprintf("%d@unknown.invalid", id)
	err := a.repo.Register(ctx, id, fmt.Sprintf("unknown-%d", id), email)
	if err != nil {
		return nil, err
	}

	a.log.Info("admin provisioned user", zap.Uint64("user", id))
	return a.findUser(ctx, usersrpc.AdminRequest{UserID: userID})
}

func (a *adminRPC) findUser(ctx context.Context, req usersrpc.AdminRequest) (*ent.User, error) {
	switch {
	case req.UserID != "":
		var uid uint64
		if _, err := fmt.Sscanf(req.UserID, "%d", &uid); err != nil {
			return nil, fmt.Errorf("user_id must be numeric")
		}
		return a.repo.FindUser(ctx, uid)
	case req.Username != "":
		return a.repo.FindUserByUsername(ctx, req.Username)
	default:
		return nil, fmt.Errorf("user_id or username required")
	}
}

func (a *adminRPC) invalidate(id uint64) {
	if err := invalidate.Publish(a.nc, a.invalidationPrefix, "status", fmt.Sprint(id)); err != nil {
		a.log.Warn("cache invalidation publish failed", zap.Error(err))
	}
}

func viewOf(u *ent.User) usersrpc.AdminUserView {
	return usersrpc.AdminUserView{
		ID:        u.ID,
		Username:  u.Username,
		IsActive:  u.IsActive,
		Status:    string(u.Status),
		Banned:    u.Banned,
		UpdatedAt: u.UpdatedAt,
	}
}

func userViewsOf(rows []*ent.User) []usersrpc.AdminUserView {
	views := make([]usersrpc.AdminUserView, 0, len(rows))
	for _, u := range rows {
		views = append(views, viewOf(u))
	}
	return views
}
