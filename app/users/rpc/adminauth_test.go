package rpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/adminuser"
	"ItsBagelBot/app/users/ent/enttest"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupAdminAuthTest(t *testing.T) (*adminAuthRPC, *ent.Client) {
	t.Helper()

	client := enttest.Open(t, "sqlite3", "file:adminauth?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	return &adminAuthRPC{db: client, log: zap.NewNop()}, client
}

type staffFixture struct {
	id     uint64
	role   adminuser.Role
	active bool
}

func createStaff(t *testing.T, client *ent.Client, fixture staffFixture) *ent.AdminUser {
	t.Helper()
	login := fmt.Sprintf("staff-%d", fixture.id)
	return client.AdminUser.Create().
		SetID(fixture.id).
		SetLogin(login).
		SetDisplayName(login).
		SetRole(fixture.role).
		SetActive(fixture.active).
		SaveX(context.Background())
}

func createAuditEntry(t *testing.T, client *ent.Client, i int, createdAt time.Time) {
	t.Helper()

	client.AdminAudit.Create().
		SetActorID(uint64(1000 + i%2)).
		SetActorLogin(fmt.Sprintf("actor-%02d", i)).
		SetAction("set_status").
		SetTarget(fmt.Sprintf("user-%02d", i)).
		SetDetail(fmt.Sprintf("detail-%02d", i)).
		SetOk(true).
		SetCreatedAt(createdAt).
		ExecX(context.Background())
}

func TestAuditListPagesResults(t *testing.T) {
	a, client := setupAdminAuthTest(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		createAuditEntry(t, client, i, base.Add(time.Duration(i)*time.Minute))
	}

	first := a.auditList(ctx, usersrpc.AuthRequest{Page: 1, Limit: auditPageSize})
	require.Empty(t, first.Error)
	require.Len(t, first.Entries, auditPageSize)
	assert.Equal(t, 1, first.Page)
	assert.Equal(t, auditPageSize, first.PageSize)
	assert.Equal(t, auditMaxPages, first.MaxPages)
	assert.True(t, first.HasMore)
	assert.Equal(t, "user-29", first.Entries[0].Target)
	assert.Equal(t, fmt.Sprintf("user-%02d", 30-auditPageSize), first.Entries[auditPageSize-1].Target)

	second := a.auditList(ctx, usersrpc.AuthRequest{Page: 2, Limit: auditPageSize})
	require.Empty(t, second.Error)
	require.Len(t, second.Entries, auditPageSize)
	assert.False(t, second.HasMore)
	assert.Equal(t, fmt.Sprintf("user-%02d", 30-auditPageSize-1), second.Entries[0].Target)
}

func TestAuditListSearchesBeforePaging(t *testing.T) {
	a, client := setupAdminAuthTest(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		createAuditEntry(t, client, i, base.Add(time.Duration(i)*time.Minute))
	}
	client.AdminAudit.Create().
		SetActorID(4242).
		SetActorLogin("itsmavey").
		SetAction("staff_upsert").
		SetTarget("needle-target").
		SetDetail("Promoted through the audit search needle").
		SetOk(true).
		SetCreatedAt(base.Add(2 * time.Hour)).
		ExecX(ctx)

	reply := a.auditList(ctx, usersrpc.AuthRequest{Page: 1, Limit: auditPageSize, Search: "NEEDLE"})
	require.Empty(t, reply.Error)
	require.Len(t, reply.Entries, 1)
	assert.Equal(t, "itsmavey", reply.Entries[0].ActorLogin)
	assert.Equal(t, "staff_upsert", reply.Entries[0].Action)
	assert.False(t, reply.HasMore)
}

func TestUpsertStaffAuthorizesStoredActorRole(t *testing.T) {
	a, client := setupAdminAuthTest(t)
	ctx := context.Background()
	createStaff(t, client, staffFixture{id: 99, role: adminuser.RoleModerator, active: true})
	createStaff(t, client, staffFixture{id: 100, role: adminuser.RoleAdmin, active: true})
	createStaff(t, client, staffFixture{id: 101, role: adminuser.RoleOwner, active: true})

	moderatorSpoof := a.upsertStaff(ctx, usersrpc.AuthRequest{
		ActorID:   "99",
		ActorRole: "owner",
		UserID:    "199",
		Login:     "new-moderator",
		Role:      "moderator",
	})
	assert.Equal(t, "forbidden: managers only", moderatorSpoof.Error)
	_, err := client.AdminUser.Get(ctx, 199)
	assert.True(t, ent.IsNotFound(err))

	spoofed := a.upsertStaff(ctx, usersrpc.AuthRequest{
		ActorID:   "100",
		ActorRole: "owner",
		UserID:    "200",
		Login:     "new-owner",
		Role:      "owner",
	})
	assert.Equal(t, "forbidden: only an owner can grant owner", spoofed.Error)
	_, err = client.AdminUser.Get(ctx, 200)
	assert.True(t, ent.IsNotFound(err))

	legitimate := a.upsertStaff(ctx, usersrpc.AuthRequest{
		ActorID:   "101",
		ActorRole: "moderator",
		UserID:    "200",
		Login:     "new-owner",
		Role:      "owner",
	})
	require.Empty(t, legitimate.Error)
	created := client.AdminUser.GetX(ctx, 200)
	assert.Equal(t, adminuser.RoleOwner, created.Role)
	assert.Equal(t, uint64(101), created.AddedBy)
}

func TestRemoveStaffAuthorizesStoredActorRole(t *testing.T) {
	a, client := setupAdminAuthTest(t)
	ctx := context.Background()
	createStaff(t, client, staffFixture{id: 100, role: adminuser.RoleAdmin, active: true})
	createStaff(t, client, staffFixture{id: 101, role: adminuser.RoleOwner, active: true})
	createStaff(t, client, staffFixture{id: 102, role: adminuser.RoleOwner, active: true})

	spoofed := a.removeStaff(ctx, usersrpc.AuthRequest{
		ActorID:   "100",
		ActorRole: "owner",
		UserID:    "102",
	})
	assert.Equal(t, "forbidden: cannot remove an owner", spoofed.Error)
	assert.True(t, client.AdminUser.GetX(ctx, 102).Active)

	legitimate := a.removeStaff(ctx, usersrpc.AuthRequest{
		ActorID:   "101",
		ActorRole: "moderator",
		UserID:    "102",
	})
	require.Empty(t, legitimate.Error)
	assert.False(t, client.AdminUser.GetX(ctx, 102).Active)
}

func TestRosterMutationsRejectInactiveActor(t *testing.T) {
	a, client := setupAdminAuthTest(t)
	ctx := context.Background()
	createStaff(t, client, staffFixture{id: 100, role: adminuser.RoleOwner, active: false})
	createStaff(t, client, staffFixture{id: 200, role: adminuser.RoleModerator, active: true})

	upsert := a.upsertStaff(ctx, usersrpc.AuthRequest{
		ActorID:   "100",
		ActorRole: "owner",
		UserID:    "201",
		Login:     "new-moderator",
		Role:      "moderator",
	})
	assert.Equal(t, "forbidden: actor is not active staff", upsert.Error)
	_, err := client.AdminUser.Get(ctx, 201)
	assert.True(t, ent.IsNotFound(err))

	remove := a.removeStaff(ctx, usersrpc.AuthRequest{
		ActorID:   "100",
		ActorRole: "owner",
		UserID:    "200",
	})
	assert.Equal(t, "forbidden: actor is not active staff", remove.Error)
	assert.True(t, client.AdminUser.GetX(ctx, 200).Active)
}
