package rpc

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/adminaudit"
	"ItsBagelBot/app/users/ent/adminuser"
	"ItsBagelBot/app/users/ent/predicate"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	dbgate "ItsBagelBot/pkg/db"
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

type adminAuthRPC struct {
	db  *ent.Client
	log *zap.Logger
}

const (
	auditPageSize     = 15
	auditMaxPages     = 25
	auditMaxSearchLen = 200
)

// SubscribeAdminAuth wires the auth.* and audit.* verbs. authPrefix defaults to
// "bagel.rpc.admin.user.auth", auditPrefix to "bagel.rpc.admin.user.audit" so
// they ride the console admin user's existing "bagel.rpc.admin.user.>" NATS
// publish permission (no broker ACL change needed).
func SubscribeAdminAuth(w Wiring, db *ent.Client, authPrefix, auditPrefix string) error {
	a := &adminAuthRPC{db: db, log: w.Log}

	routes := map[string]func(context.Context, usersrpc.AuthRequest) usersrpc.AuthReply{
		authPrefix + ".check":   a.check,
		authPrefix + ".list":    a.listStaff,
		authPrefix + ".upsert":  a.upsertStaff,
		authPrefix + ".remove":  a.removeStaff,
		auditPrefix + ".append": a.auditAppend,
		auditPrefix + ".list":   a.auditList,
	}
	for subject, handle := range routes {
		if err := bus.QueueSubscribeJSON[usersrpc.AuthRequest, usersrpc.AuthReply](w.NC, subject, w.Queue, 3*time.Second, w.App, w.Log, handle); err != nil {
			return err
		}
	}
	return nil
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

func authError(msg string) usersrpc.AuthReply { return usersrpc.AuthReply{Error: msg} }

// check resolves whether the Twitch subject is active staff. When login/
// display_name are supplied (sign-in path), it refreshes them so the allowlist
// stays current after a Twitch rename.
func (a *adminAuthRPC) check(ctx context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
	id, err := parseID(req.UserID)
	if err != nil {
		return authError(err.Error())
	}
	row, err := a.findStaff(ctx, id)
	if ent.IsNotFound(err) {
		return usersrpc.AuthReply{Admin: false}
	}
	if err != nil {
		return authError(err.Error())
	}
	if !row.Active {
		return usersrpc.AuthReply{Admin: false}
	}

	row = a.refreshIdentity(ctx, row, req)
	return usersrpc.AuthReply{
		Admin:       true,
		Role:        string(row.Role),
		Login:       row.Login,
		DisplayName: row.DisplayName,
	}
}

// staleIdentity reports whether the sign-in carries a fresh login or display
// name the stored row does not yet reflect (a Twitch rename).
func staleIdentity(row *ent.AdminUser, req usersrpc.AuthRequest) bool {
	if req.Login == "" {
		return false
	}
	if req.Login != row.Login {
		return true
	}
	return req.DisplayName != "" && req.DisplayName != row.DisplayName
}

// refreshIdentity persists the sign-in's login/display name when they have
// drifted, returning the saved row (or the original on a write failure — a
// stale label must never fail an auth check).
func (a *adminAuthRPC) refreshIdentity(ctx context.Context, row *ent.AdminUser, req usersrpc.AuthRequest) *ent.AdminUser {
	if !staleIdentity(row, req) {
		return row
	}
	upd := a.db.AdminUser.UpdateOneID(row.ID).SetLogin(req.Login)
	if req.DisplayName != "" {
		upd = upd.SetDisplayName(req.DisplayName)
	}
	saved, err := dbgate.WithQuery(ctx, func(ctx context.Context) (*ent.AdminUser, error) {
		return upd.Save(ctx)
	})
	if err != nil {
		return row
	}
	return saved
}

func (a *adminAuthRPC) findStaff(ctx context.Context, id uint64) (*ent.AdminUser, error) {
	return dbgate.WithQuery(ctx, func(ctx context.Context) (*ent.AdminUser, error) {
		return a.db.AdminUser.Query().Where(adminuser.IDEQ(id)).Only(ctx)
	})
}

func (a *adminAuthRPC) listStaff(ctx context.Context, _ usersrpc.AuthRequest) usersrpc.AuthReply {
	rows, err := dbgate.WithQuery(ctx, func(ctx context.Context) ([]*ent.AdminUser, error) {
		return a.db.AdminUser.Query().
			Order(ent.Asc(adminuser.FieldCreatedAt)).
			All(ctx)
	})
	if err != nil {
		return authError(err.Error())
	}
	out := make([]usersrpc.AdminAcctView, 0, len(rows))
	for _, r := range rows {
		out = append(out, adminViewOf(r))
	}
	return usersrpc.AuthReply{Admins: out}
}

// upsertStaff creates or modifies a staff member. Enforces the role ladder:
//   - actor must be a manager (admin/owner);
//   - only an owner may set a target's role to owner;
//   - only an owner may modify an existing owner.
func (a *adminAuthRPC) upsertStaff(ctx context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
	id, newRole, errMsg := a.validateUpsert(ctx, req)
	if errMsg != "" {
		return authError(errMsg)
	}

	addedBy, _ := parseID(req.ActorID)
	display := req.DisplayName
	if display == "" {
		display = req.Login
	}
	row := staffRow{id: id, login: req.Login, display: display, role: newRole, addedBy: addedBy}
	if err := upsertStaffRow(ctx, a.db, row); err != nil {
		return authError(err.Error())
	}
	a.log.Info("staff upsert", zap.Uint64("id", id), zap.String("role", string(newRole)), zap.Uint64("by", addedBy))
	return a.listStaff(ctx, usersrpc.AuthRequest{})
}

// validateUpsert runs every role-ladder guard for an upsert and resolves the
// effective new role. A non-empty errMsg means the request is rejected.
func (a *adminAuthRPC) validateUpsert(ctx context.Context, req usersrpc.AuthRequest) (id uint64, newRole adminuser.Role, errMsg string) {
	actor, errMsg := a.resolveActiveActor(ctx, req.ActorID)
	if errMsg != "" {
		return 0, "", errMsg
	}
	actorRole := actor.Role
	if !isManager(actorRole) {
		return 0, "", "forbidden: managers only"
	}
	id, err := parseID(req.UserID)
	if err != nil {
		return 0, "", err.Error()
	}
	if req.Login == "" {
		return 0, "", "login required"
	}

	newRole, errMsg = resolveNewRole(req.Role, actorRole)
	if errMsg != "" {
		return 0, "", errMsg
	}
	// No self-modification: staff cannot change their own role.
	if actor.ID == id {
		return 0, "", "forbidden: cannot change your own role"
	}
	if errMsg := a.guardExistingTarget(ctx, id, actorRole); errMsg != "" {
		return 0, "", errMsg
	}
	return id, newRole, ""
}

// resolveActiveActor loads the actor from the staff allowlist. ActorRole is
// deliberately not consulted: authorization must be based on the persisted
// role so a caller cannot elevate itself by forging request metadata.
func (a *adminAuthRPC) resolveActiveActor(ctx context.Context, rawID string) (*ent.AdminUser, string) {
	actorID, err := parseID(rawID)
	if err != nil {
		if rawID == "" {
			return nil, "actor_id required"
		}
		return nil, "actor_id must be numeric"
	}
	actor, err := a.findStaff(ctx, actorID)
	if ent.IsNotFound(err) {
		return nil, "forbidden: actor is not active staff"
	}
	if err != nil {
		return nil, err.Error()
	}
	if !actor.Active {
		return nil, "forbidden: actor is not active staff"
	}
	return actor, ""
}

// resolveNewRole resolves the effective role for an upsert (empty defaults to
// moderator), validates it, and enforces that only an owner may grant owner.
func resolveNewRole(raw string, actorRole adminuser.Role) (adminuser.Role, string) {
	newRole := adminuser.Role(raw)
	if raw == "" {
		newRole = adminuser.RoleModerator
	}
	if err := adminuser.RoleValidator(newRole); err != nil {
		return "", "role must be moderator, admin or owner"
	}
	if newRole == adminuser.RoleOwner && actorRole != adminuser.RoleOwner {
		return "", "forbidden: only an owner can grant owner"
	}
	return newRole, ""
}

// guardExistingTarget applies the guards that depend on the target's current
// role: an owner's role is immutable, and an admin cannot modify another admin.
// A missing target is fine (this is a create).
func (a *adminAuthRPC) guardExistingTarget(ctx context.Context, id uint64, actorRole adminuser.Role) string {
	existing, err := a.findStaff(ctx, id)
	if ent.IsNotFound(err) {
		return ""
	}
	if err != nil {
		return err.Error()
	}
	if existing.Role == adminuser.RoleOwner {
		return "forbidden: an owner's role cannot be changed"
	}
	if actorRole == adminuser.RoleAdmin && existing.Role == adminuser.RoleAdmin {
		return "forbidden: admins cannot change another admin"
	}
	return ""
}

// removeStaff soft-disables a staff member (active=false) so historical audit
// rows keep resolving the actor. Owners may only be removed by owners, and the
// last active owner can never be removed (lockout guard).
func (a *adminAuthRPC) removeStaff(ctx context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
	id, errMsg := a.validateRemove(ctx, req)
	if errMsg != "" {
		return authError(errMsg)
	}

	if err := dbgate.WithExec(ctx, func(ctx context.Context) error {
		return a.db.AdminUser.UpdateOneID(id).SetActive(false).Exec(ctx)
	}); err != nil {
		return authError(err.Error())
	}
	actorID, _ := parseID(req.ActorID)
	a.log.Info("staff removed", zap.Uint64("id", id), zap.Uint64("by", actorID))
	return a.listStaff(ctx, usersrpc.AuthRequest{})
}

// validateRemove runs the removal guards and returns the target id. A non-empty
// errMsg means the request is rejected.
func (a *adminAuthRPC) validateRemove(ctx context.Context, req usersrpc.AuthRequest) (uint64, string) {
	actor, errMsg := a.resolveActiveActor(ctx, req.ActorID)
	if errMsg != "" {
		return 0, errMsg
	}
	actorRole := actor.Role
	if !isManager(actorRole) {
		return 0, "forbidden: managers only"
	}
	id, err := parseID(req.UserID)
	if err != nil {
		return 0, err.Error()
	}
	if actor.ID == id {
		return 0, "forbidden: cannot remove yourself"
	}

	target, err := a.findStaff(ctx, id)
	if ent.IsNotFound(err) {
		return 0, "staff not found"
	}
	if err != nil {
		return 0, err.Error()
	}
	return id, a.guardTargetRemoval(ctx, actorRole, target)
}

// guardTargetRemoval applies the guards that depend on the target's role: an
// admin cannot remove another admin, and an owner may only be removed under the
// stricter owner rule (see guardOwnerRemoval).
func (a *adminAuthRPC) guardTargetRemoval(ctx context.Context, actorRole adminuser.Role, target *ent.AdminUser) string {
	if actorRole == adminuser.RoleAdmin && target.Role == adminuser.RoleAdmin {
		return "forbidden: admins cannot remove another admin"
	}
	if target.Role == adminuser.RoleOwner {
		return a.guardOwnerRemoval(ctx, actorRole)
	}
	return ""
}

// guardOwnerRemoval blocks removing an owner unless the actor is an owner and
// at least one other active owner would remain.
func (a *adminAuthRPC) guardOwnerRemoval(ctx context.Context, actorRole adminuser.Role) string {
	if actorRole != adminuser.RoleOwner {
		return "forbidden: cannot remove an owner"
	}
	owners, err := dbgate.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return a.db.AdminUser.Query().
			Where(adminuser.RoleEQ(adminuser.RoleOwner), adminuser.ActiveEQ(true)).
			Count(ctx)
	})
	if err != nil {
		return err.Error()
	}
	if owners <= 1 {
		return "cannot remove the last owner"
	}
	return ""
}

func (a *adminAuthRPC) auditAppend(ctx context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
	actorID, err := parseID(req.ActorID)
	if err != nil {
		return authError("actor_id: " + err.Error())
	}
	if req.ActorLogin == "" || req.Action == "" {
		return authError("actor_login and action required")
	}
	_, err = dbgate.WithQuery(ctx, func(ctx context.Context) (*ent.AdminAudit, error) {
		return a.db.AdminAudit.Create().
			SetActorID(actorID).
			SetActorLogin(req.ActorLogin).
			SetAction(req.Action).
			SetTarget(req.Target).
			SetDetail(req.Detail).
			SetOk(req.OK).
			SetError(req.Err).
			Save(ctx)
	})
	if err != nil {
		return authError(err.Error())
	}
	return usersrpc.AuthReply{}
}

func (a *adminAuthRPC) auditList(ctx context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
	q := a.db.AdminAudit.Query().Order(ent.Desc(adminaudit.FieldCreatedAt), ent.Desc(adminaudit.FieldID))
	// Optional actor filter: lazy-load a single operator's own history without
	// shipping the whole log (actor_filter = their Twitch id).
	if req.ActorFilter != "" {
		aid, err := parseID(req.ActorFilter)
		if err != nil {
			return authError("actor_filter: " + err.Error())
		}
		q = q.Where(adminaudit.ActorIDEQ(aid))
	}
	if search := normalizeAuditSearch(req.Search); search != "" {
		q = q.Where(auditSearchPredicate(search))
	}

	if req.Page > 0 {
		return a.auditListPage(ctx, q, req.Page, req.Limit)
	}
	return a.auditListAll(ctx, q, req.Limit)
}

func auditListLimit(limit int) int {
	if limit <= 0 || limit > 200 {
		return 50
	}
	return limit
}

// auditListAll returns the newest rows up to a bounded limit (no pagination).
func (a *adminAuthRPC) auditListAll(ctx context.Context, q *ent.AdminAuditQuery, limit int) usersrpc.AuthReply {
	rows, err := dbgate.WithQuery(ctx, func(ctx context.Context) ([]*ent.AdminAudit, error) {
		return q.Limit(auditListLimit(limit)).All(ctx)
	})
	if err != nil {
		return authError(err.Error())
	}
	return usersrpc.AuthReply{Entries: auditViewsOf(rows)}
}

// auditListPage returns one clamped page, fetching one extra row (except on the
// last page) to compute has-more without a count query.
func (a *adminAuthRPC) auditListPage(ctx context.Context, q *ent.AdminAuditQuery, page, limit int) usersrpc.AuthReply {
	page = clamp(page, 1, auditMaxPages)
	pageSize := clamp(auditListLimit(limit), 1, auditPageSize)
	fetchLimit := pageSize
	if page < auditMaxPages {
		fetchLimit++
	}
	rows, err := dbgate.WithQuery(ctx, func(ctx context.Context) ([]*ent.AdminAudit, error) {
		return q.Offset((page - 1) * pageSize).Limit(fetchLimit).All(ctx)
	})
	if err != nil {
		return authError(err.Error())
	}
	hasMore := page < auditMaxPages && len(rows) > pageSize
	if hasMore {
		rows = rows[:pageSize]
	}
	return usersrpc.AuthReply{
		Entries:  auditViewsOf(rows),
		Page:     page,
		PageSize: pageSize,
		MaxPages: auditMaxPages,
		HasMore:  hasMore,
	}
}

// clamp constrains v to [lo, hi].
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func normalizeAuditSearch(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > auditMaxSearchLen {
		return string(runes[:auditMaxSearchLen])
	}
	return s
}

func auditSearchPredicate(search string) predicate.AdminAudit {
	predicates := []predicate.AdminAudit{
		adminaudit.ActorLoginContainsFold(search),
		adminaudit.ActionContainsFold(search),
		adminaudit.TargetContainsFold(search),
		adminaudit.DetailContainsFold(search),
		adminaudit.ErrorContainsFold(search),
	}
	if actorID, err := strconv.ParseUint(search, 10, 64); err == nil {
		predicates = append(predicates, adminaudit.ActorIDEQ(actorID))
	}
	return adminaudit.Or(predicates...)
}

func auditViewsOf(rows []*ent.AdminAudit) []usersrpc.AuditView {
	out := make([]usersrpc.AuditView, 0, len(rows))
	for _, r := range rows {
		out = append(out, usersrpc.AuditView{
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
	return out
}

// StaffSeed lists the bootstrap operators to guarantee on startup
// (OWNER_BOOTSTRAP_IDS / ADMIN_BOOTSTRAP_IDS).
type StaffSeed struct {
	Owners []uint64
	Admins []uint64
}

// SeedStaff bootstraps owners and admins from the seed lists. Existing rows are
// re-activated and promoted to at least the seeded role, so a redeploy can
// never lock out or demote a bootstrap operator.
func SeedStaff(ctx context.Context, db *ent.Client, seed StaffSeed, log *zap.Logger) error {
	s := staffSeeder{db: db, log: log}
	if err := s.seedRole(ctx, seed.Owners, adminuser.RoleOwner); err != nil {
		return err
	}
	return s.seedRole(ctx, seed.Admins, adminuser.RoleAdmin)
}

type staffSeeder struct {
	db  *ent.Client
	log *zap.Logger
}

func (s staffSeeder) seedRole(ctx context.Context, ids []uint64, role adminuser.Role) error {
	for _, id := range ids {
		if err := s.seedOne(ctx, id, role); err != nil {
			return err
		}
	}
	return nil
}

func (s staffSeeder) seedOne(ctx context.Context, id uint64, role adminuser.Role) error {
	existing, err := dbgate.WithQuery(ctx, func(ctx context.Context) (*ent.AdminUser, error) {
		return s.db.AdminUser.Query().Where(adminuser.IDEQ(id)).Only(ctx)
	})
	if err == nil {
		upd := s.db.AdminUser.UpdateOneID(id).SetActive(true)
		// Only ever promote via seed; never demote a manually-elevated row.
		if rank(role) > rank(existing.Role) {
			upd = upd.SetRole(role)
		}
		return dbgate.WithExec(ctx, func(ctx context.Context) error {
			return upd.Exec(ctx)
		})
	}
	if !ent.IsNotFound(err) {
		return err
	}
	login := fmt.Sprintf("bootstrap-%d", id)
	if err := upsertStaffRow(ctx, s.db, staffRow{id: id, login: login, display: login, role: role}); err != nil {
		return err
	}
	s.log.Info("seeded bootstrap staff", zap.Uint64("id", id), zap.String("role", string(role)))
	return nil
}

// staffRow is the persisted shape of one staff member (create or update).
type staffRow struct {
	id      uint64
	login   string
	display string
	role    adminuser.Role
	addedBy uint64
}

func upsertStaffRow(ctx context.Context, db *ent.Client, row staffRow) error {
	_, err := dbgate.WithQuery(ctx, func(ctx context.Context) (*ent.AdminUser, error) {
		return db.AdminUser.Query().Where(adminuser.IDEQ(row.id)).Only(ctx)
	})
	if err == nil {
		return updateStaffRow(ctx, db, row)
	}
	if !ent.IsNotFound(err) {
		return err
	}
	return dbgate.WithExec(ctx, func(ctx context.Context) error {
		err := db.AdminUser.Create().
			SetID(row.id).
			SetLogin(row.login).
			SetDisplayName(row.display).
			SetRole(row.role).
			SetAddedBy(row.addedBy).
			SetActive(true).
			Exec(ctx)
		// A concurrent create wins the race: fall back to updating the row it made.
		if ent.IsConstraintError(err) {
			return updateStaffRow(ctx, db, row)
		}
		return err
	})
}

func updateStaffRow(ctx context.Context, db *ent.Client, row staffRow) error {
	return dbgate.WithExec(ctx, func(ctx context.Context) error {
		return db.AdminUser.UpdateOneID(row.id).
			SetLogin(row.login).
			SetDisplayName(row.display).
			SetRole(row.role).
			SetActive(true).
			Exec(ctx)
	})
}

func adminViewOf(r *ent.AdminUser) usersrpc.AdminAcctView {
	return usersrpc.AdminAcctView{
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
