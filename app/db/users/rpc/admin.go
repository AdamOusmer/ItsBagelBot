// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/adminuser"
	"ItsBagelBot/app/db/users/ent/tokens"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/invalidate"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"
)

type adminRPC struct {
	repo               *repository.Users
	gate               staffGate
	nc                 *nats.Conn
	invalidationPrefix string
	log                *zap.Logger
}

type adminVerb struct {
	name   string
	min    adminuser.Role
	handle func(context.Context, usersrpc.AdminRequest) usersrpc.AdminReply
}

func (a *adminRPC) verbs() []adminVerb {
	const (
		mod   = adminuser.RoleModerator
		admin = adminuser.RoleAdmin
		owner = adminuser.RoleOwner
	)
	return []adminVerb{
		{"get", mod, a.get},
		{"list", mod, a.list},
		{"stats", mod, a.stats},
		{"enrollment", mod, a.enrollment},
		{"overview", mod, a.overview},
		{"token_status", mod, a.tokenStatus},
		{"ban", mod, a.ban},
		{"unban", mod, a.unban},
		{"set_status", admin, a.setStatus},
		{"set_active", admin, a.setActive},
		{"set_creator_code", admin, a.setCreatorCode},
		{"test.set", admin, a.setTestAccount},
		{"reset", admin, a.reset},
		{"token_set", admin, a.tokenSet},
		{"token_clear", admin, a.tokenClear},
		{"delete", owner, a.delete},
	}
}

func (a *adminRPC) authorize(ctx context.Context, actorID string, min adminuser.Role) domainrpc.Refusal {
	actor, errMsg := a.gate.resolveActiveActor(ctx, actorID)
	if errMsg != "" {
		return domainrpc.Refused(domainrpc.CodeForbidden, errMsg)
	}
	if rank(actor.Role) < rank(min) {
		return domainrpc.Refused(domainrpc.CodeForbidden, "forbidden: "+string(min)+" role or higher required")
	}
	return domainrpc.Refusal{}
}

func (a *adminRPC) guarded(v adminVerb) func(context.Context, usersrpc.AdminRequest) usersrpc.AdminReply {
	return func(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
		if r := a.authorize(ctx, req.ActorID, v.min); r.Code != domainrpc.CodeOK {
			return adminError(r)
		}
		return v.handle(ctx, req)
	}
}

const (
	adminUserPageSize = repository.AdminUserPageSize
	adminUserMaxPages = repository.AdminUserMaxPages
)

type AdminConfig struct {
	Prefix             string
	InternalGetSubject string
	InvalidationPrefix string
}

func SubscribeAdmin(w Wiring, db *ent.Client, cfg AdminConfig) error {
	a := &adminRPC{
		repo:               w.Repo,
		gate:               staffGate{db: db},
		nc:                 w.NC,
		invalidationPrefix: cfg.InvalidationPrefix,
		log:                w.Log,
	}

	table := a.verbs()
	bound := make([]bus.Verb[usersrpc.AdminRequest, usersrpc.AdminReply], 0, len(table))
	for _, v := range table {
		bound = append(bound, bus.At(v.name, a.guarded(v)))
	}
	if err := bus.ServeVerbs(w.Within(adminBudget), cfg.Prefix, bound...); err != nil {
		return err
	}

	return bus.Serve(w.Within(adminBudget), cfg.InternalGetSubject, a.get)
}

var storeRules = []domainrpc.Rule{
	domainrpc.Is(repository.ErrUserNotFound, domainrpc.CodeNotFound),
	domainrpc.Is(repository.ErrNoContactEmail, domainrpc.CodeNotFound),
	domainrpc.When(ent.IsNotFound, domainrpc.CodeNotFound),
}

func refusal(err error) domainrpc.Refusal { return bus.Classify(err, storeRules...) }

func adminError(r domainrpc.Refusal) usersrpc.AdminReply { return usersrpc.AdminReply{Refusal: r} }

type mutation struct {
	logMsg string
	write  func(context.Context, uint64) error
	reply  func(context.Context, uint64) usersrpc.AdminReply
	fields []zap.Field
}

func (a *adminRPC) mutate(ctx context.Context, req usersrpc.AdminRequest, m mutation) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminError(refusal(err))
	}
	if err := m.write(ctx, u.ID); err != nil {
		return adminError(refusal(err))
	}
	a.invalidate(u.ID)
	a.log.Info(m.logMsg, append([]zap.Field{zap.Uint64("user", u.ID)}, m.fields...)...)
	return m.reply(ctx, u.ID)
}

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
		return adminError(refusal(err))
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
	rows, err := a.repo.ListUsers(ctx, repository.AdminUserQuery{
		Search: req.Search,
		State:  req.State,
		Limit:  adminListLimit(req.Limit),
	})
	if err != nil {
		return adminError(refusal(err))
	}
	return usersrpc.AdminReply{Users: userViewsOf(rows)}
}

func (a *adminRPC) listPage(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	page := clamp(req.Page, 1, adminUserMaxPages)
	pageSize := clamp(adminListLimit(req.Limit), 1, adminUserPageSize)
	fetchLimit := pageSize
	if page < adminUserMaxPages {
		fetchLimit++
	}
	rows, err := a.repo.ListUsers(ctx, repository.AdminUserQuery{
		Search: req.Search,
		State:  req.State,
		Limit:  fetchLimit,
		Offset: (page - 1) * pageSize,
	})
	if err != nil {
		return adminError(refusal(err))
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
		return adminError(refusal(err))
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

const (
	enrollmentDefaultDays = 30
	enrollmentMaxDays     = 90
)

func (a *adminRPC) enrollment(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	days := req.Days
	if days <= 0 {
		days = enrollmentDefaultDays
	}
	series, err := a.repo.EnrollmentSeries(ctx, clamp(days, 1, enrollmentMaxDays))
	if err != nil {
		return adminError(refusal(err))
	}
	stats := a.stats(ctx, req)
	if stats.Error != "" {
		return stats
	}
	view := usersrpc.AdminEnrollmentView{Days: enrollmentDaysOf(series), Stats: *stats.Stats}
	return usersrpc.AdminReply{Enrollment: &view}
}

func enrollmentDaysOf(series []repository.EnrollmentDay) []usersrpc.AdminEnrollmentDay {
	days := make([]usersrpc.AdminEnrollmentDay, 0, len(series))
	for _, d := range series {
		days = append(days, usersrpc.AdminEnrollmentDay{Date: d.Date, Count: d.Count})
	}
	return days
}

func (a *adminRPC) setStatus(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	log := monitor.TxnLogger(ctx, a.log)
	u, err := a.findOrProvision(ctx, req)
	if err != nil {
		return adminError(refusal(err))
	}

	status := user.Status(req.Status)
	if err := user.StatusValidator(status); err != nil {
		return adminError(domainrpc.Refused(domainrpc.CodeInvalid, "status must be free, paid or vip"))
	}
	expiresAt, err := parseExpiresAt(req.ExpiresAt)
	if err != nil {
		return adminError(refusal(err))
	}
	if err := a.repo.SetAdminStatus(ctx, u.ID, status, expiresAt); err != nil {
		return adminError(refusal(err))
	}

	a.invalidate(u.ID)
	log.Info("admin status change",
		zap.Uint64("user", u.ID), zap.String("status", req.Status))
	return a.get(ctx, idRequest(u.ID))
}

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

func (a *adminRPC) findOrProvision(ctx context.Context, req usersrpc.AdminRequest) (*ent.User, error) {
	u, err := a.findUser(ctx, req)
	if errors.Is(err, repository.ErrUserNotFound) && req.UserID != "" {
		return a.provision(ctx, req.UserID)
	}
	return u, err
}

func (a *adminRPC) setActive(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	return a.mutate(ctx, req, mutation{
		logMsg: "admin set active",
		write:  func(ctx context.Context, id uint64) error { return a.repo.SetActive(ctx, id, req.Active) },
		reply:  a.getByID,
		fields: []zap.Field{zap.Bool("active", req.Active)},
	})
}

func (a *adminRPC) setCreatorCode(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	log := monitor.TxnLogger(ctx, a.log)
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminError(refusal(err))
	}

	if err := a.repo.SetCreatorCode(ctx, u.ID, req.CreatorCode); err != nil {
		return adminError(refusal(err))
	}

	a.invalidate(u.ID)
	log.Info("admin set creator code", zap.Uint64("user", u.ID), zap.Bool("cleared", req.CreatorCode == ""))
	return a.get(ctx, usersrpc.AdminRequest{UserID: fmt.Sprint(u.ID)})
}

func (a *adminRPC) setTestAccount(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminError(refusal(err))
	}
	actorID, err := bus.UserID(req.ActorID)
	if err != nil {
		return adminError(refusal(err))
	}
	if err := a.repo.SetTestAccount(ctx, u.ID, req.TestAccount, actorID); err != nil {
		return adminError(refusal(err))
	}
	a.invalidate(u.ID)
	monitor.TxnLogger(ctx, a.log).Info("admin test-account marker change",
		zap.Uint64("user", u.ID), zap.Bool("enabled", req.TestAccount))
	return a.get(ctx, idRequest(u.ID))
}

func (a *adminRPC) ban(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	return a.mutate(ctx, req, mutation{
		logMsg: "admin ban",
		write:  func(ctx context.Context, id uint64) error { return a.repo.SetBanned(ctx, id, true) },
		reply:  a.getByID,
	})
}

func (a *adminRPC) unban(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	return a.mutate(ctx, req, mutation{
		logMsg: "admin unban",
		write:  func(ctx context.Context, id uint64) error { return a.repo.SetBanned(ctx, id, false) },
		reply:  a.getByID,
	})
}

func (a *adminRPC) reset(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	return a.mutate(ctx, req, mutation{
		logMsg: "admin state reset",
		write:  func(ctx context.Context, id uint64) error { return a.repo.ResetTokens(ctx, id) },
		reply:  a.getByID,
	})
}

func (a *adminRPC) tokenSet(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findOrProvision(ctx, req)
	if err != nil {
		return adminError(refusal(err))
	}

	if err := a.repo.UpsertToken(ctx, u.ID, tokens.TypeUserToken, tokens.PlatformTwitch,
		[]byte(req.AccessToken), []byte(req.RefreshToken), nil); err != nil {
		return adminError(refusal(err))
	}

	log := monitor.TxnLogger(ctx, a.log)
	a.invalidate(u.ID)
	log.Info("admin token set", zap.Uint64("user", u.ID))
	return a.tokenStatus(ctx, idRequest(u.ID))
}

func (a *adminRPC) tokenStatus(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminError(refusal(err))
	}

	present, err := a.repo.HasToken(ctx, u.ID, tokens.TypeUserToken, tokens.PlatformTwitch)
	if err != nil {
		return adminError(refusal(err))
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
		return adminError(refusal(err))
	}

	if err := a.repo.Delete(ctx, u.ID); err != nil {
		return adminError(refusal(err))
	}

	monitor.TxnLogger(ctx, a.log).Info("admin user delete", zap.Uint64("user", u.ID))
	return usersrpc.AdminReply{}
}

func (a *adminRPC) provision(ctx context.Context, userID string) (*ent.User, error) {
	id, err := bus.UserID(userID)
	if err != nil {
		return nil, err
	}

	email := fmt.Sprintf("%d@unknown.invalid", id)
	if err := a.repo.Register(ctx, id, fmt.Sprintf("unknown-%d", id), "", email); err != nil {
		return nil, err
	}

	a.log.Info("admin provisioned user", zap.Uint64("user", id))
	return a.findUser(ctx, usersrpc.AdminRequest{UserID: userID})
}

func (a *adminRPC) findUser(ctx context.Context, req usersrpc.AdminRequest) (*ent.User, error) {
	switch {
	case req.UserID != "":
		uid, err := bus.UserID(req.UserID)
		if err != nil {
			return nil, err
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
		TestAccount:               u.TestAccount,
		CreatedAt:                 u.CreatedAt,
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
