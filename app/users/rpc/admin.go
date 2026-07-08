package rpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
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

func SubscribeAdmin(w Wiring, prefix, invalidationPrefix string) error {
	a := &adminRPC{
		repo:               w.Repo,
		nc:                 w.NC,
		invalidationPrefix: invalidationPrefix,
		log:                w.Log,
	}

	verbs := map[string]func(context.Context, usersrpc.AdminRequest) usersrpc.AdminReply{
		"get":              a.get,
		"list":             a.list,
		"stats":            a.stats,
		"overview":         a.overview,
		"set_status":       a.setStatus,
		"set_active":       a.setActive,
		"set_creator_code": a.setCreatorCode,
		"ban":              a.ban,
		"unban":            a.unban,
		"reset":            a.reset,
		"token_set":        a.tokenSet,
		"token_status":     a.tokenStatus,
		"token_clear":      a.tokenClear,
		"delete":           a.delete,
	}
	for verb, handle := range verbs {
		subject := prefix + "." + verb
		if err := bus.QueueSubscribeJSON[usersrpc.AdminRequest, usersrpc.AdminReply](a.nc, subject, w.Queue, 3*time.Second, w.App, w.Log, handle); err != nil {
			return err
		}
	}
	return nil
}

func adminError(msg string) usersrpc.AdminReply { return usersrpc.AdminReply{Error: msg} }

// mutation names one per-user write verb: the log line it emits, the repo
// write it applies, the refreshed reply it returns (a user view or a token
// view), and any extra structured log fields.
type mutation struct {
	logMsg string
	write  func(context.Context, uint64) error
	reply  func(context.Context, uint64) usersrpc.AdminReply
	fields []zap.Field
}

// mutate resolves the request's user, applies the write, invalidates the
// cache, logs, and returns the verb's refreshed reply. Any failure short-
// circuits with the service error.
func (a *adminRPC) mutate(ctx context.Context, req usersrpc.AdminRequest, m mutation) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminError(err.Error())
	}
	if err := m.write(ctx, u.ID); err != nil {
		return adminError(err.Error())
	}
	a.invalidate(u.ID)
	a.log.Info(m.logMsg, append([]zap.Field{zap.Uint64("user", u.ID)}, m.fields...)...)
	return m.reply(ctx, u.ID)
}

// getByID / tokenStatusByID are the two refreshed replies verbs pick from.
func (a *adminRPC) getByID(ctx context.Context, id uint64) usersrpc.AdminReply {
	return a.get(ctx, idRequest(id))
}

func (a *adminRPC) tokenStatusByID(ctx context.Context, id uint64) usersrpc.AdminReply {
	return a.tokenStatus(ctx, idRequest(id))
}

func idRequest(id uint64) usersrpc.AdminRequest {
	return usersrpc.AdminRequest{UserID: fmt.Sprint(id)}
}

func (a *adminRPC) get(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}
	view := viewOf(u)
	return usersrpc.AdminReply{User: &view}
}

func adminListLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 20
	}
	return limit
}

func (a *adminRPC) list(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	if req.Page > 0 {
		return a.listPage(ctx, req)
	}
	rows, err := a.repo.ListUsers(ctx, req.Search, adminListLimit(req.Limit), 0)
	if err != nil {
		return adminError(err.Error())
	}
	return usersrpc.AdminReply{Users: userViewsOf(rows)}
}

// listPage returns one clamped page, fetching one extra row (except on the last
// page) to compute has-more without a separate count query.
func (a *adminRPC) listPage(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	page := clamp(req.Page, 1, adminUserMaxPages)
	pageSize := clamp(adminListLimit(req.Limit), 1, adminUserPageSize)
	fetchLimit := pageSize
	if page < adminUserMaxPages {
		fetchLimit++
	}
	rows, err := a.repo.ListUsers(ctx, req.Search, fetchLimit, (page-1)*pageSize)
	if err != nil {
		return adminError(err.Error())
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
	u, err := a.findOrProvision(ctx, req)
	if err != nil {
		return adminError(err.Error())
	}

	status := user.Status(req.Status)
	if err := user.StatusValidator(status); err != nil {
		return adminError("status must be free, paid or vip")
	}
	expiresAt, err := parseExpiresAt(req.ExpiresAt)
	if err != nil {
		return adminError(err.Error())
	}
	if err := a.repo.SetAdminStatus(ctx, u.ID, status, expiresAt); err != nil {
		return adminError(err.Error())
	}

	a.invalidate(u.ID)
	a.log.Info("admin status change",
		zap.Uint64("user", u.ID), zap.String("status", req.Status))
	return a.get(ctx, idRequest(u.ID))
}

// parseExpiresAt parses the optional RFC3339 grant end date; an empty value
// means no expiry (nil).
func parseExpiresAt(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("expires_at must be an RFC3339 timestamp")
	}
	return &parsed, nil
}

// findOrProvision resolves the request's user, provisioning a fresh row when a
// user_id is given but no row exists yet (the bot-account install path).
func (a *adminRPC) findOrProvision(ctx context.Context, req usersrpc.AdminRequest) (*ent.User, error) {
	u, err := a.findUser(ctx, req)
	if errors.Is(err, repository.ErrUserNotFound) && req.UserID != "" {
		return a.provision(ctx, req.UserID)
	}
	return u, err
}

// setActive flips whether the bot serves this broadcaster. Inactive users
// project to standard tier and the ingress drops their traffic.
func (a *adminRPC) setActive(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	return a.mutate(ctx, req, mutation{
		logMsg: "admin set active",
		write:  func(ctx context.Context, id uint64) error { return a.repo.SetActive(ctx, id, req.Active) },
		reply:  a.getByID,
		fields: []zap.Field{zap.Bool("active", req.Active)},
	})
}

func (a *adminRPC) setCreatorCode(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	if err := a.repo.SetCreatorCode(ctx, u.ID, req.CreatorCode); err != nil {
		return usersrpc.AdminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin set creator code", zap.Uint64("user", u.ID), zap.Bool("cleared", req.CreatorCode == ""))
	return a.get(ctx, usersrpc.AdminRequest{UserID: fmt.Sprint(u.ID)})
}

// ban blocks the user from the service entirely. The ingress drops banned
// users, so their traffic never reaches a worker.
func (a *adminRPC) ban(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	return a.mutate(ctx, req, mutation{
		logMsg: "admin ban",
		write:  func(ctx context.Context, id uint64) error { return a.repo.SetBanned(ctx, id, true) },
		reply:  a.getByID,
	})
}

// unban lifts a previous ban, allowing the user's traffic through again.
func (a *adminRPC) unban(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	return a.mutate(ctx, req, mutation{
		logMsg: "admin unban",
		write:  func(ctx context.Context, id uint64) error { return a.repo.SetBanned(ctx, id, false) },
		reply:  a.getByID,
	})
}

// reset clears all tokens for the user. Configs and timers are managed
// externally, so token wipe is the scope of this operation.
func (a *adminRPC) reset(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	return a.mutate(ctx, req, mutation{
		logMsg: "admin state reset",
		write:  func(ctx context.Context, id uint64) error { return a.repo.ResetTokens(ctx, id) },
		reply:  a.getByID,
	})
}

// tokenSet stores (or replaces) the user's Twitch OAuth token. This is how
// the operator installs the bot account's own token; the row is provisioned
// on first sight so the bot account does not need to onboard like a
// broadcaster.
func (a *adminRPC) tokenSet(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findOrProvision(ctx, req)
	if err != nil {
		return adminError(err.Error())
	}

	if err := a.repo.UpsertToken(ctx, u.ID, tokens.TypeUserToken, tokens.PlatformTwitch,
		[]byte(req.AccessToken), []byte(req.RefreshToken)); err != nil {
		return adminError(err.Error())
	}

	a.invalidate(u.ID)
	a.log.Info("admin token set", zap.Uint64("user", u.ID))
	return a.tokenStatus(ctx, idRequest(u.ID))
}

func (a *adminRPC) tokenStatus(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminError(err.Error())
	}

	present, err := a.repo.HasToken(ctx, u.ID, tokens.TypeUserToken, tokens.PlatformTwitch)
	if err != nil {
		return adminError(err.Error())
	}

	return usersrpc.AdminReply{Token: &usersrpc.AdminTokenView{Present: present}}
}

func (a *adminRPC) tokenClear(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	return a.mutate(ctx, req, mutation{
		logMsg: "admin token cleared",
		write: func(ctx context.Context, id uint64) error {
			return a.repo.ClearToken(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch)
		},
		reply: a.tokenStatusByID,
	})
}

func (a *adminRPC) delete(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminError(err.Error())
	}

	if err := a.repo.Delete(ctx, u.ID); err != nil {
		return adminError(err.Error())
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
		ID:                        u.ID,
		Username:                  u.Username,
		IsActive:                  u.IsActive,
		Status:                    string(u.Status),
		Banned:                    u.Banned,
		CreatorCode:               u.CreatorCode,
		SubscriptionExpiresAt:     u.SubscriptionExpiresAt,
		SubscriptionSource:        u.SubscriptionSource,
		SubscriptionRef:           u.SubscriptionRef,
		SubscriptionCancelPending: u.SubscriptionCancelPending,
		UpdatedAt:                 u.UpdatedAt,
	}
}

func userViewsOf(rows []*ent.User) []usersrpc.AdminUserView {
	views := make([]usersrpc.AdminUserView, 0, len(rows))
	for _, u := range rows {
		views = append(views, viewOf(u))
	}
	return views
}
