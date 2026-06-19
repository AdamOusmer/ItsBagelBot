package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/predicate"
	"ItsBagelBot/app/users/ent/tokens"
	"ItsBagelBot/app/users/ent/user"
	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/pkg/bus"
)

type adminUserView struct {
	ID        uint64    `json:"id"`
	Username  string    `json:"username"`
	IsActive  bool      `json:"is_active"`
	Status    string    `json:"status"`
	Banned    bool      `json:"banned"`
	UpdatedAt time.Time `json:"updated_at"`
}

type adminStats struct {
	TotalUsers   int `json:"total_users"`
	ActiveUsers  int `json:"active_users"`
	PremiumUsers int `json:"premium_users"`
	VIPUsers     int `json:"vip_users"`
	PaidUsers    int `json:"paid_users"`
}

type adminTokenView struct {
	Present bool `json:"present"`
}

type adminReply struct {
	User     *adminUserView  `json:"user,omitempty"`
	Users    []adminUserView `json:"users,omitempty"`
	Stats    *adminStats     `json:"stats,omitempty"`
	Token    *adminTokenView `json:"token,omitempty"`
	Page     int             `json:"page,omitempty"`
	PageSize int             `json:"page_size,omitempty"`
	MaxPages int             `json:"max_pages,omitempty"`
	HasMore  bool            `json:"has_more,omitempty"`
	Error    string          `json:"error,omitempty"`
}

type adminRequest struct {
	UserID       string `json:"user_id"`
	Username     string `json:"username"`
	Status       string `json:"status"`
	Active       bool   `json:"active"`
	Limit        int    `json:"limit"`
	Page         int    `json:"page"`
	Search       string `json:"search"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type adminRPC struct {
	db                  *ent.Client
	repo                *repository.Users
	nc                  *nats.Conn
	invalidationSubject string
	log                 *zap.Logger
}

const (
	adminUserPageSize     = 15
	adminUserMaxPages     = 25
	adminUserMaxSearchLen = 200
)

func SubscribeAdmin(nc *nats.Conn, db *ent.Client, repo *repository.Users, prefix, invalidationSubject, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	a := &adminRPC{
		db:                  db,
		repo:                repo,
		nc:                  nc,
		invalidationSubject: invalidationSubject,
		log:                 log,
	}

	verbs := map[string]func(context.Context, adminRequest) adminReply{
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
		if err := bus.QueueSubscribeJSON[adminRequest, adminReply](a.nc, subject, queueGroup, 3*time.Second, app, log, handle); err != nil {
			return err
		}
	}
	return nil
}

func (a *adminRPC) get(ctx context.Context, req adminRequest) adminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminReply{Error: err.Error()}
	}
	view := viewOf(u)
	return adminReply{User: &view}
}

func (a *adminRPC) list(ctx context.Context, req adminRequest) adminReply {
	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := a.db.User.Query().Order(ent.Desc(user.FieldUpdatedAt), ent.Desc(user.FieldID))
	if search := normalizeAdminUserSearch(req.Search); search != "" {
		q = q.Where(adminUserSearchPredicate(search))
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
		rows, err := q.Offset((page - 1) * pageSize).Limit(fetchLimit).All(ctx)
		if err != nil {
			return adminReply{Error: err.Error()}
		}
		hasMore := page < adminUserMaxPages && len(rows) > pageSize
		if hasMore {
			rows = rows[:pageSize]
		}
		return adminReply{
			Users:    userViewsOf(rows),
			Page:     page,
			PageSize: pageSize,
			MaxPages: adminUserMaxPages,
			HasMore:  hasMore,
		}
	}
	rows, err := q.Limit(limit).All(ctx)
	if err != nil {
		return adminReply{Error: err.Error()}
	}
	return adminReply{Users: userViewsOf(rows)}
}

func (a *adminRPC) overview(ctx context.Context, req adminRequest) adminReply {
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

func (a *adminRPC) stats(ctx context.Context, _ adminRequest) adminReply {
	total, err := a.db.User.Query().Count(ctx)
	if err != nil {
		return adminReply{Error: err.Error()}
	}
	active, err := a.db.User.Query().Where(user.IsActiveEQ(true)).Count(ctx)
	if err != nil {
		return adminReply{Error: err.Error()}
	}
	paid, err := a.db.User.Query().Where(user.StatusEQ(user.StatusPaid)).Count(ctx)
	if err != nil {
		return adminReply{Error: err.Error()}
	}
	vip, err := a.db.User.Query().Where(user.StatusEQ(user.StatusVip)).Count(ctx)
	if err != nil {
		return adminReply{Error: err.Error()}
	}
	stats := adminStats{
		TotalUsers:   total,
		ActiveUsers:  active,
		PremiumUsers: paid + vip,
		VIPUsers:     vip,
		PaidUsers:    paid,
	}
	return adminReply{Stats: &stats}
}

func (a *adminRPC) setStatus(ctx context.Context, req adminRequest) adminReply {
	u, err := a.findUser(ctx, req)
	if err != nil && err.Error() == "user not found" && req.UserID != "" {
		u, err = a.provision(ctx, req.UserID)
	}
	if err != nil {
		return adminReply{Error: err.Error()}
	}

	status := user.Status(req.Status)
	if err := user.StatusValidator(status); err != nil {
		return adminReply{Error: "status must be free, paid or vip"}
	}

	if err := a.repo.SetStatus(ctx, u.ID, status); err != nil {
		return adminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin status change",
		zap.Uint64("user", u.ID), zap.String("status", req.Status))
	return a.get(ctx, adminRequest{UserID: fmt.Sprint(u.ID)})
}

// setActive flips whether the bot serves this broadcaster. Inactive users
// project to standard tier and the ingress drops their traffic.
func (a *adminRPC) setActive(ctx context.Context, req adminRequest) adminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminReply{Error: err.Error()}
	}

	if err := a.repo.SetActive(ctx, u.ID, req.Active); err != nil {
		return adminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin set active", zap.Uint64("user", u.ID), zap.Bool("active", req.Active))
	return a.get(ctx, adminRequest{UserID: fmt.Sprint(u.ID)})
}

// ban blocks the user from the service entirely. The ingress drops banned
// users, so their traffic never reaches a worker.
func (a *adminRPC) ban(ctx context.Context, req adminRequest) adminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminReply{Error: err.Error()}
	}

	if err := a.repo.SetBanned(ctx, u.ID, true); err != nil {
		return adminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin ban", zap.Uint64("user", u.ID))
	return a.get(ctx, adminRequest{UserID: fmt.Sprint(u.ID)})
}

// unban lifts a previous ban, allowing the user's traffic through again.
func (a *adminRPC) unban(ctx context.Context, req adminRequest) adminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminReply{Error: err.Error()}
	}

	if err := a.repo.SetBanned(ctx, u.ID, false); err != nil {
		return adminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin unban", zap.Uint64("user", u.ID))
	return a.get(ctx, adminRequest{UserID: fmt.Sprint(u.ID)})
}

// reset in the new architecture just clears tokens for now, as configs/timers are external
func (a *adminRPC) reset(ctx context.Context, req adminRequest) adminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminReply{Error: err.Error()}
	}

	if _, err := a.db.Tokens.Delete().
		Where(tokens.HasUserWith(user.IDEQ(u.ID))).
		Exec(ctx); err != nil {
		return adminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin state reset", zap.Uint64("user", u.ID))
	return a.get(ctx, adminRequest{UserID: fmt.Sprint(u.ID)})
}

// tokenSet stores (or replaces) the user's Twitch OAuth token. This is how
// the operator installs the bot account's own token; the row is provisioned
// on first sight so the bot account does not need to onboard like a
// broadcaster.
func (a *adminRPC) tokenSet(ctx context.Context, req adminRequest) adminReply {
	u, err := a.findUser(ctx, req)
	if err != nil && err.Error() == "user not found" && req.UserID != "" {
		u, err = a.provision(ctx, req.UserID)
	}
	if err != nil {
		return adminReply{Error: err.Error()}
	}

	if err := a.repo.UpsertToken(ctx, u.ID, tokens.TypeUserToken, tokens.PlatformTwitch,
		[]byte(req.AccessToken), []byte(req.RefreshToken)); err != nil {
		return adminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin token set", zap.Uint64("user", u.ID))
	return a.tokenStatus(ctx, adminRequest{UserID: fmt.Sprint(u.ID)})
}

func (a *adminRPC) tokenStatus(ctx context.Context, req adminRequest) adminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminReply{Error: err.Error()}
	}

	present, err := a.db.Tokens.Query().
		Where(
			tokens.TypeEQ(tokens.TypeUserToken),
			tokens.PlatformEQ(tokens.PlatformTwitch),
			tokens.HasUserWith(user.IDEQ(u.ID)),
		).
		Exist(ctx)
	if err != nil {
		return adminReply{Error: err.Error()}
	}

	return adminReply{Token: &adminTokenView{Present: present}}
}

func (a *adminRPC) tokenClear(ctx context.Context, req adminRequest) adminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminReply{Error: err.Error()}
	}

	if _, err := a.db.Tokens.Delete().
		Where(
			tokens.TypeEQ(tokens.TypeUserToken),
			tokens.PlatformEQ(tokens.PlatformTwitch),
			tokens.HasUserWith(user.IDEQ(u.ID)),
		).
		Exec(ctx); err != nil {
		return adminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin token cleared", zap.Uint64("user", u.ID))
	return a.tokenStatus(ctx, adminRequest{UserID: fmt.Sprint(u.ID)})
}

func (a *adminRPC) delete(ctx context.Context, req adminRequest) adminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminReply{Error: err.Error()}
	}

	if err := a.repo.Delete(ctx, u.ID); err != nil {
		return adminReply{Error: err.Error()}
	}

	a.log.Info("admin user delete", zap.Uint64("user", u.ID))
	return adminReply{}
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
	return a.findUser(ctx, adminRequest{UserID: userID})
}

func (a *adminRPC) findUser(ctx context.Context, req adminRequest) (*ent.User, error) {
	q := a.db.User.Query()
	switch {
	case req.UserID != "":
		id, err := strconv.ParseUint(req.UserID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("user_id must be numeric")
		}
		q = q.Where(user.IDEQ(id))
	case req.Username != "":
		q = q.Where(user.UsernameEQ(req.Username))
	default:
		return nil, fmt.Errorf("user_id or username required")
	}
	u, err := q.Only(ctx)
	if ent.IsNotFound(err) {
		return nil, fmt.Errorf("user not found")
	}
	return u, err
}

func (a *adminRPC) invalidate(id uint64) {
	body, _ := json.Marshal(map[string]string{"broadcaster_id": fmt.Sprint(id)})
	if err := a.nc.Publish(a.invalidationSubject, body); err != nil {
		a.log.Warn("cache invalidation publish failed", zap.Error(err))
	}
}

func viewOf(u *ent.User) adminUserView {
	return adminUserView{
		ID:        u.ID,
		Username:  u.Username,
		IsActive:  u.IsActive,
		Status:    string(u.Status),
		Banned:    u.Banned,
		UpdatedAt: u.UpdatedAt,
	}
}

func userViewsOf(rows []*ent.User) []adminUserView {
	views := make([]adminUserView, 0, len(rows))
	for _, u := range rows {
		views = append(views, viewOf(u))
	}
	return views
}

func normalizeAdminUserSearch(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > adminUserMaxSearchLen {
		return string(runes[:adminUserMaxSearchLen])
	}
	return s
}

func adminUserSearchPredicate(search string) predicate.User {
	predicates := []predicate.User{
		user.UsernameContainsFold(search),
	}
	if id, err := strconv.ParseUint(search, 10, 64); err == nil {
		predicates = append(predicates, user.IDEQ(id))
	}
	return user.Or(predicates...)
}
