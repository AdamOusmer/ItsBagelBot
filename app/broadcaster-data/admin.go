// Admin RPC verbs for the operator tool. These are the only write paths into
// broadcaster state besides the dashboard's own onboarding, and they exist
// here because this service owns the schema: the admin tool never opens
// MySQL, it asks us over NATS.
//
// Status model: the lane tier lives in the Configs JSON under "tier"
// (premium|standard, read by tierOf and the ingress). Grants additionally
// record how the premium was obtained under "premium_kind":
//
//   - "vip":  granted by the operator, permanent premium
//   - "paid": paid premium (Tebex)
//
// "standard" clears both. Every mutation publishes the broadcaster cache
// invalidation key so the ingress fleet drops its cached tier immediately.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/internal/db/ent"
	"ItsBagelBot/internal/db/ent/timers"
	"ItsBagelBot/internal/db/ent/user"
)

type adminUserView struct {
	ID          uint64    `json:"id"`
	Username    string    `json:"username"`
	IsActive    bool      `json:"is_active"`
	Tier        string    `json:"tier"`
	PremiumKind string    `json:"premium_kind,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type adminStats struct {
	TotalUsers   int `json:"total_users"`
	ActiveUsers  int `json:"active_users"`
	PremiumUsers int `json:"premium_users"`
	VIPUsers     int `json:"vip_users"`
	PaidUsers    int `json:"paid_users"`
}

type adminReply struct {
	User  *adminUserView  `json:"user,omitempty"`
	Users []adminUserView `json:"users,omitempty"`
	Stats *adminStats     `json:"stats,omitempty"`
	Error string          `json:"error,omitempty"`
}

type adminRequest struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Status   string `json:"status"`
	Limit    int    `json:"limit"`
}

type adminRPC struct {
	db                  *ent.Client
	nc                  *nats.Conn
	invalidationSubject string
	log                 *zap.Logger
}

// subscribe registers the admin verbs under prefix, all on the service queue
// group so one replica answers.
func (a *adminRPC) subscribe(prefix string) error {
	verbs := map[string]func(context.Context, adminRequest) adminReply{
		"get":        a.get,
		"list":       a.list,
		"stats":      a.stats,
		"set_status": a.setStatus,
		"reset":      a.reset,
	}
	for verb, handle := range verbs {
		handle := handle
		subject := prefix + "." + verb
		if _, err := a.nc.QueueSubscribe(subject, rpcQueueGroup, func(msg *nats.Msg) {
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
		WithConfigs().
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
	rows, err := a.db.User.Query().
		WithConfigs().
		All(ctx)
	if err != nil {
		return adminReply{Error: err.Error()}
	}

	stats := adminStats{TotalUsers: len(rows)}
	for _, u := range rows {
		if u.IsActive {
			stats.ActiveUsers++
		}
		view := viewOf(u)
		if view.Tier == "premium" {
			stats.PremiumUsers++
		}
		switch view.PremiumKind {
		case "vip":
			stats.VIPUsers++
		case "paid":
			stats.PaidUsers++
		}
	}
	return adminReply{Stats: &stats}
}

// setStatus moves the user between vip (permanent premium), paid (paid
// premium) and standard. This is the money path's admin override: it writes
// through immediately and invalidates the fleet cache.
//
// A grant by numeric id for a user this schema has never seen provisions the
// row (placeholder identity): nothing else writes this table yet, and the
// tier must exist for the lanes to honor the grant. A later onboarding flow
// can fill in the real username/email on the same id.
func (a *adminRPC) setStatus(ctx context.Context, req adminRequest) adminReply {
	u, err := a.findUser(ctx, req)
	if err != nil && err.Error() == "user not found" && req.UserID != "" {
		u, err = a.provision(ctx, req.UserID)
	}
	if err != nil {
		return adminReply{Error: err.Error()}
	}

	cfg := configsOf(u)
	switch req.Status {
	case "vip":
		cfg["tier"] = "premium"
		cfg["premium_kind"] = "vip"
	case "paid":
		cfg["tier"] = "premium"
		cfg["premium_kind"] = "paid"
	case "standard":
		cfg["tier"] = "standard"
		delete(cfg, "premium_kind")
	default:
		return adminReply{Error: "status must be vip, paid or standard"}
	}

	if err := a.saveConfigs(ctx, u, cfg); err != nil {
		return adminReply{Error: err.Error()}
	}
	a.invalidate(u.ID)
	a.log.Info("admin status change",
		zap.Uint64("user", u.ID), zap.String("status", req.Status))
	return a.get(ctx, adminRequest{UserID: fmt.Sprint(u.ID)})
}

// reset clears the user's settings (configs except the tier keys) and their
// timers. The account, grants, tier and transaction history stay.
func (a *adminRPC) reset(ctx context.Context, req adminRequest) adminReply {
	u, err := a.findUser(ctx, req)
	if err != nil {
		return adminReply{Error: err.Error()}
	}

	old := configsOf(u)
	fresh := map[string]any{}
	if tier, ok := old["tier"]; ok {
		fresh["tier"] = tier
	}
	if kind, ok := old["premium_kind"]; ok {
		fresh["premium_kind"] = kind
	}
	if err := a.saveConfigs(ctx, u, fresh); err != nil {
		return adminReply{Error: err.Error()}
	}

	if _, err := a.db.Timers.Delete().
		Where(timers.HasUserWith(user.ID(u.ID))).
		Exec(ctx); err != nil {
		return adminReply{Error: err.Error()}
	}

	a.invalidate(u.ID)
	a.log.Info("admin state reset", zap.Uint64("user", u.ID))
	return a.get(ctx, adminRequest{UserID: fmt.Sprint(u.ID)})
}

func (a *adminRPC) provision(ctx context.Context, userID string) (*ent.User, error) {
	var id uint64
	if _, err := fmt.Sscanf(userID, "%d", &id); err != nil {
		return nil, fmt.Errorf("user_id must be numeric")
	}
	_, err := a.db.User.Create().
		SetID(id).
		SetUsername(fmt.Sprintf("unknown-%d", id)).
		SetEmail(fmt.Sprintf("%d@unknown.invalid", id)).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	a.log.Info("admin provisioned user", zap.Uint64("user", id))
	return a.findUser(ctx, adminRequest{UserID: userID})
}

func (a *adminRPC) findUser(ctx context.Context, req adminRequest) (*ent.User, error) {
	q := a.db.User.Query().WithConfigs()
	switch {
	case req.UserID != "":
		var id uint64
		if _, err := fmt.Sscanf(req.UserID, "%d", &id); err != nil {
			return nil, fmt.Errorf("user_id must be numeric")
		}
		q = q.Where(user.ID(id))
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

func (a *adminRPC) saveConfigs(ctx context.Context, u *ent.User, cfg map[string]any) error {
	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if u.Edges.Configs == nil {
		_, err = a.db.Configs.Create().SetConfigs(body).SetUser(u).Save(ctx)
		return err
	}
	return a.db.Configs.UpdateOne(u.Edges.Configs).SetConfigs(body).Exec(ctx)
}

// invalidate tells every ingress replica to drop its cached tier for this
// broadcaster (same contract as Ingress.CacheInvalidator).
func (a *adminRPC) invalidate(id uint64) {
	body, _ := json.Marshal(map[string]string{"broadcaster_id": fmt.Sprint(id)})
	if err := a.nc.Publish(a.invalidationSubject, body); err != nil {
		a.log.Warn("cache invalidation publish failed", zap.Error(err))
	}
}

func viewOf(u *ent.User) adminUserView {
	view := adminUserView{
		ID:        u.ID,
		Username:  u.Username,
		IsActive:  u.IsActive,
		Tier:      "standard",
		UpdatedAt: u.UpdatedAt,
	}
	cfg := configsOf(u)
	if tier, _ := cfg["tier"].(string); tier == "premium" {
		view.Tier = "premium"
	}
	if kind, _ := cfg["premium_kind"].(string); kind != "" {
		view.PremiumKind = kind
	}
	return view
}

func configsOf(u *ent.User) map[string]any {
	cfg := map[string]any{}
	if u.Edges.Configs != nil && len(u.Edges.Configs.Configs) > 0 {
		_ = json.Unmarshal(u.Edges.Configs.Configs, &cfg)
	}
	return cfg
}
