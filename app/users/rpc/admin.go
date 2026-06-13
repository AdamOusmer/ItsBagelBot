package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/tokens"
	"ItsBagelBot/app/users/ent/user"
	"ItsBagelBot/app/users/repository"
)

type adminUserView struct {
	ID        uint64    `json:"id"`
	Username  string    `json:"username"`
	IsActive  bool      `json:"is_active"`
	Status    string    `json:"status"`
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
	User  *adminUserView  `json:"user,omitempty"`
	Users []adminUserView `json:"users,omitempty"`
	Stats *adminStats     `json:"stats,omitempty"`
	Token *adminTokenView `json:"token,omitempty"`
	Error string          `json:"error,omitempty"`
}

type adminRequest struct {
	UserID       string `json:"user_id"`
	Username     string `json:"username"`
	Status       string `json:"status"`
	Limit        int    `json:"limit"`
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

func SubscribeAdmin(nc *nats.Conn, db *ent.Client, repo *repository.Users, prefix, invalidationSubject, queueGroup string, log *zap.Logger) error {
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
		"set_status":   a.setStatus,
		"reset":        a.reset,
		"token_set":    a.tokenSet,
		"token_status": a.tokenStatus,
		"token_clear":  a.tokenClear,
	}
	for verb, handle := range verbs {
		handle := handle
		subject := prefix + "." + verb
		if _, err := a.nc.QueueSubscribe(subject, queueGroup, func(msg *nats.Msg) {
			var req adminRequest
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				respond(msg, adminReply{Error: "bad request"})
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			respond(msg, handle(ctx, req))
		}); err != nil {
			return fmt.Errorf("subscribe %s: %w", subject, err)
		}
	}
	return nil
}

func respond(msg *nats.Msg, reply adminReply) {
	body, _ := json.Marshal(reply)
	_ = msg.Respond(body)
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
	rows, err := a.db.User.Query().
		Order(ent.Desc(user.FieldUpdatedAt)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return adminReply{Error: err.Error()}
	}
	views := make([]adminUserView, 0, len(rows))
	for _, u := range rows {
		views = append(views, viewOf(u))
	}
	return adminReply{Users: views}
}

func (a *adminRPC) stats(ctx context.Context, _ adminRequest) adminReply {
	rows, err := a.db.User.Query().All(ctx)
	if err != nil {
		return adminReply{Error: err.Error()}
	}

	stats := adminStats{TotalUsers: len(rows)}
	for _, u := range rows {
		if u.IsActive {
			stats.ActiveUsers++
		}
		if u.Status == user.StatusPaid || u.Status == user.StatusVip {
			stats.PremiumUsers++
		}
		if u.Status == user.StatusVip {
			stats.VIPUsers++
		}
		if u.Status == user.StatusPaid {
			stats.PaidUsers++
		}
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
		UpdatedAt: u.UpdatedAt,
	}
}
