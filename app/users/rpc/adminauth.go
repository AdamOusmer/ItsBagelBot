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
	"ItsBagelBot/app/users/ent/adminaudit"
	"ItsBagelBot/app/users/ent/adminuser"
)

// adminauth serves the admin console's authorization + audit surface. It is the
// DB-backed replacement for the old static ADMIN_USER_IDS env allowlist: the
// console resolves whether a Twitch sign-in is staff via auth.check, manages the
// staff roster via auth.upsert / auth.remove, and records every mutating
// operator action via audit.append. The tailnet is the network boundary; this
// staff allowlist is the identity boundary.
//
// Role ladder (moderator < admin < owner) is enforced here as defense in depth,
// not only in the console: only managers (admin/owner) may change the roster,
// and only an owner may create, modify, or remove an owner.

type authRequest struct {
	// actor: who is performing a roster change (set by the console from session).
	ActorID   string `json:"actor_id"`
	ActorRole string `json:"actor_role"`

	// target / identity
	UserID      string `json:"user_id"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`

	// audit.append
	ActorLogin string `json:"actor_login"`
	Action     string `json:"action"`
	Target     string `json:"target"`
	Detail     string `json:"detail"`
	OK         bool   `json:"ok"`
	Err        string `json:"error"`

	// audit.list / auth.list
	Limit int `json:"limit"`
}

type adminAcctView struct {
	ID          uint64    `json:"id"`
	Login       string    `json:"login"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	Active      bool      `json:"active"`
	AddedBy     uint64    `json:"added_by"`
	CreatedAt   time.Time `json:"created_at"`
}

type auditView struct {
	ID         int       `json:"id"`
	ActorID    uint64    `json:"actor_id"`
	ActorLogin string    `json:"actor_login"`
	Action     string    `json:"action"`
	Target     string    `json:"target,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	OK         bool      `json:"ok"`
	Err        string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type authReply struct {
	Admin       bool            `json:"admin"`
	Role        string          `json:"role,omitempty"`
	Login       string          `json:"login,omitempty"`
	DisplayName string          `json:"display_name,omitempty"`
	Admins      []adminAcctView `json:"admins,omitempty"`
	Entries     []auditView     `json:"entries,omitempty"`
	Error       string          `json:"error,omitempty"`
}

type adminAuthRPC struct {
	db  *ent.Client
	log *zap.Logger
}

// SubscribeAdminAuth wires the auth.* and audit.* verbs. authPrefix defaults to
// "bagel.rpc.admin.user.auth", auditPrefix to "bagel.rpc.admin.user.audit" so
// they ride the console admin user's existing "bagel.rpc.admin.user.>" NATS
// publish permission (no broker ACL change needed).
func SubscribeAdminAuth(nc *nats.Conn, db *ent.Client, authPrefix, auditPrefix, queueGroup string, log *zap.Logger) error {
	a := &adminAuthRPC{db: db, log: log}

	routes := map[string]func(context.Context, authRequest) authReply{
		authPrefix + ".check":   a.check,
		authPrefix + ".list":    a.listStaff,
		authPrefix + ".upsert":  a.upsertStaff,
		authPrefix + ".remove":  a.removeStaff,
		auditPrefix + ".append": a.auditAppend,
		auditPrefix + ".list":   a.auditList,
	}
	for subject, handle := range routes {
		handle := handle
		if _, err := nc.QueueSubscribe(subject, queueGroup, func(msg *nats.Msg) {
			var req authRequest
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				respondAuth(msg, authReply{Error: "bad request"})
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			respondAuth(msg, handle(ctx, req))
		}); err != nil {
			return fmt.Errorf("subscribe %s: %w", subject, err)
		}
	}
	return nil
}

func respondAuth(msg *nats.Msg, reply authReply) {
	body, _ := json.Marshal(reply)
	_ = msg.Respond(body)
}

// rank orders the role ladder for comparisons. Unknown roles rank lowest.
func rank(r adminuser.Role) int {
	switch r {
	case adminuser.RoleOwner:
		return 3
	case adminuser.RoleAdmin:
		return 2
	case adminuser.RoleModerator:
		return 1
	default:
		return 0
	}
}

func isManager(r adminuser.Role) bool { return r == adminuser.RoleAdmin || r == adminuser.RoleOwner }

// check resolves whether the Twitch subject is active staff. When login/
// display_name are supplied (sign-in path), it refreshes them so the allowlist
// stays current after a Twitch rename.
func (a *adminAuthRPC) check(ctx context.Context, req authRequest) authReply {
	id, err := parseID(req.UserID)
	if err != nil {
		return authReply{Error: err.Error()}
	}
	row, err := a.db.AdminUser.Query().Where(adminuser.IDEQ(id)).Only(ctx)
	if ent.IsNotFound(err) {
		return authReply{Admin: false}
	}
	if err != nil {
		return authReply{Error: err.Error()}
	}
	if !row.Active {
		return authReply{Admin: false}
	}

	if req.Login != "" && (req.Login != row.Login || (req.DisplayName != "" && req.DisplayName != row.DisplayName)) {
		upd := a.db.AdminUser.UpdateOneID(id).SetLogin(req.Login)
		if req.DisplayName != "" {
			upd = upd.SetDisplayName(req.DisplayName)
		}
		if saved, err := upd.Save(ctx); err == nil {
			row = saved
		}
	}

	return authReply{
		Admin:       true,
		Role:        string(row.Role),
		Login:       row.Login,
		DisplayName: row.DisplayName,
	}
}

func (a *adminAuthRPC) listStaff(ctx context.Context, _ authRequest) authReply {
	rows, err := a.db.AdminUser.Query().
		Order(ent.Asc(adminuser.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return authReply{Error: err.Error()}
	}
	out := make([]adminAcctView, 0, len(rows))
	for _, r := range rows {
		out = append(out, adminViewOf(r))
	}
	return authReply{Admins: out}
}

// upsertStaff creates or modifies a staff member. Enforces the role ladder:
//   - actor must be a manager (admin/owner);
//   - only an owner may set a target's role to owner;
//   - only an owner may modify an existing owner.
func (a *adminAuthRPC) upsertStaff(ctx context.Context, req authRequest) authReply {
	actorRole := adminuser.Role(req.ActorRole)
	if !isManager(actorRole) {
		return authReply{Error: "forbidden: managers only"}
	}
	id, err := parseID(req.UserID)
	if err != nil {
		return authReply{Error: err.Error()}
	}
	if req.Login == "" {
		return authReply{Error: "login required"}
	}
	newRole := adminuser.Role(req.Role)
	if req.Role == "" {
		newRole = adminuser.RoleModerator
	}
	if err := adminuser.RoleValidator(newRole); err != nil {
		return authReply{Error: "role must be moderator, admin or owner"}
	}

	// Only an owner may grant the owner role.
	if newRole == adminuser.RoleOwner && actorRole != adminuser.RoleOwner {
		return authReply{Error: "forbidden: only an owner can grant owner"}
	}

	// No self-modification: staff cannot change their own role.
	if actorID, _ := parseID(req.ActorID); actorID == id {
		return authReply{Error: "forbidden: cannot change your own role"}
	}

	// Existing-target guards.
	if existing, err := a.db.AdminUser.Query().Where(adminuser.IDEQ(id)).Only(ctx); err == nil {
		// An owner's role is immutable (cannot be changed by anyone).
		if existing.Role == adminuser.RoleOwner {
			return authReply{Error: "forbidden: an owner's role cannot be changed"}
		}
		// An admin cannot modify another admin; only an owner manages admins.
		if actorRole == adminuser.RoleAdmin && existing.Role == adminuser.RoleAdmin {
			return authReply{Error: "forbidden: admins cannot change another admin"}
		}
	} else if !ent.IsNotFound(err) {
		return authReply{Error: err.Error()}
	}

	addedBy, _ := parseID(req.ActorID)
	display := req.DisplayName
	if display == "" {
		display = req.Login
	}
	if err := upsertStaffRow(ctx, a.db, id, req.Login, display, newRole, addedBy); err != nil {
		return authReply{Error: err.Error()}
	}
	a.log.Info("staff upsert", zap.Uint64("id", id), zap.String("role", string(newRole)), zap.Uint64("by", addedBy))
	return a.listStaff(ctx, authRequest{})
}

// removeStaff soft-disables a staff member (active=false) so historical audit
// rows keep resolving the actor. Owners may only be removed by owners, and the
// last active owner can never be removed (lockout guard).
func (a *adminAuthRPC) removeStaff(ctx context.Context, req authRequest) authReply {
	actorRole := adminuser.Role(req.ActorRole)
	if !isManager(actorRole) {
		return authReply{Error: "forbidden: managers only"}
	}
	id, err := parseID(req.UserID)
	if err != nil {
		return authReply{Error: err.Error()}
	}
	// No self-removal.
	if actorID, _ := parseID(req.ActorID); actorID == id {
		return authReply{Error: "forbidden: cannot remove yourself"}
	}

	target, err := a.db.AdminUser.Query().Where(adminuser.IDEQ(id)).Only(ctx)
	if ent.IsNotFound(err) {
		return authReply{Error: "staff not found"}
	}
	if err != nil {
		return authReply{Error: err.Error()}
	}

	// An admin manages only moderators; only an owner may remove an admin.
	if actorRole == adminuser.RoleAdmin && target.Role == adminuser.RoleAdmin {
		return authReply{Error: "forbidden: admins cannot remove another admin"}
	}

	if target.Role == adminuser.RoleOwner {
		if actorRole != adminuser.RoleOwner {
			return authReply{Error: "forbidden: cannot remove an owner"}
		}
		owners, err := a.db.AdminUser.Query().
			Where(adminuser.RoleEQ(adminuser.RoleOwner), adminuser.ActiveEQ(true)).
			Count(ctx)
		if err != nil {
			return authReply{Error: err.Error()}
		}
		if owners <= 1 {
			return authReply{Error: "cannot remove the last owner"}
		}
	}

	if err := a.db.AdminUser.UpdateOneID(id).SetActive(false).Exec(ctx); err != nil {
		return authReply{Error: err.Error()}
	}
	a.log.Info("staff removed", zap.Uint64("id", id), zap.String("by_role", req.ActorRole))
	return a.listStaff(ctx, authRequest{})
}

func (a *adminAuthRPC) auditAppend(ctx context.Context, req authRequest) authReply {
	actorID, err := parseID(req.ActorID)
	if err != nil {
		return authReply{Error: "actor_id: " + err.Error()}
	}
	if req.ActorLogin == "" || req.Action == "" {
		return authReply{Error: "actor_login and action required"}
	}
	_, err = a.db.AdminAudit.Create().
		SetActorID(actorID).
		SetActorLogin(req.ActorLogin).
		SetAction(req.Action).
		SetTarget(req.Target).
		SetDetail(req.Detail).
		SetOk(req.OK).
		SetError(req.Err).
		Save(ctx)
	if err != nil {
		return authReply{Error: err.Error()}
	}
	return authReply{}
}

func (a *adminAuthRPC) auditList(ctx context.Context, req authRequest) authReply {
	limit := req.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := a.db.AdminAudit.Query().
		Order(ent.Desc(adminaudit.FieldCreatedAt)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return authReply{Error: err.Error()}
	}
	out := make([]auditView, 0, len(rows))
	for _, r := range rows {
		out = append(out, auditView{
			ID:         r.ID,
			ActorID:    r.ActorID,
			ActorLogin: r.ActorLogin,
			Action:     r.Action,
			Target:     r.Target,
			Detail:     r.Detail,
			OK:         r.Ok,
			Err:        r.Error,
			CreatedAt:  r.CreatedAt,
		})
	}
	return authReply{Entries: out}
}

// SeedStaff bootstraps owners and admins from id lists (OWNER_BOOTSTRAP_IDS /
// ADMIN_BOOTSTRAP_IDS). Existing rows are re-activated and promoted to at least
// the seeded role, so a redeploy can never lock out or demote a bootstrap
// operator.
func SeedStaff(ctx context.Context, db *ent.Client, owners, admins []uint64, log *zap.Logger) error {
	if err := seedRole(ctx, db, owners, adminuser.RoleOwner, log); err != nil {
		return err
	}
	return seedRole(ctx, db, admins, adminuser.RoleAdmin, log)
}

func seedRole(ctx context.Context, db *ent.Client, ids []uint64, role adminuser.Role, log *zap.Logger) error {
	for _, id := range ids {
		existing, err := db.AdminUser.Query().Where(adminuser.IDEQ(id)).Only(ctx)
		if err == nil {
			upd := db.AdminUser.UpdateOneID(id).SetActive(true)
			// Only ever promote via seed; never demote a manually-elevated row.
			if rank(role) > rank(existing.Role) {
				upd = upd.SetRole(role)
			}
			if err := upd.Exec(ctx); err != nil {
				return err
			}
			continue
		}
		if !ent.IsNotFound(err) {
			return err
		}
		login := fmt.Sprintf("bootstrap-%d", id)
		if err := upsertStaffRow(ctx, db, id, login, login, role, 0); err != nil {
			return err
		}
		log.Info("seeded bootstrap staff", zap.Uint64("id", id), zap.String("role", string(role)))
	}
	return nil
}

func upsertStaffRow(ctx context.Context, db *ent.Client, id uint64, login, display string, role adminuser.Role, addedBy uint64) error {
	_, err := db.AdminUser.Query().Where(adminuser.IDEQ(id)).Only(ctx)
	if err == nil {
		return db.AdminUser.UpdateOneID(id).
			SetLogin(login).
			SetDisplayName(display).
			SetRole(role).
			SetActive(true).
			Exec(ctx)
	}
	if !ent.IsNotFound(err) {
		return err
	}
	return db.AdminUser.Create().
		SetID(id).
		SetLogin(login).
		SetDisplayName(display).
		SetRole(role).
		SetAddedBy(addedBy).
		SetActive(true).
		Exec(ctx)
}

func adminViewOf(r *ent.AdminUser) adminAcctView {
	return adminAcctView{
		ID:          r.ID,
		Login:       r.Login,
		DisplayName: r.DisplayName,
		Role:        string(r.Role),
		Active:      r.Active,
		AddedBy:     r.AddedBy,
		CreatedAt:   r.CreatedAt,
	}
}

func parseID(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("user_id required")
	}
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("user_id must be numeric")
	}
	return id, nil
}
