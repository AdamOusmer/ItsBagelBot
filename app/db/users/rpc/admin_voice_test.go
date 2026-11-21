// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"fmt"
	"testing"
	"time"

	adminent "ItsBagelBot/app/db/admin/ent"
	"ItsBagelBot/app/db/admin/ent/adminaudit"
	"ItsBagelBot/app/db/admin/ent/enttest"
	adminrpc "ItsBagelBot/app/db/admin/rpc"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/adminauth"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAdminVoiceUsageRequiresActiveAdmin(t *testing.T) {
	h := voiceAdminHarness(t)
	req := usersrpc.AdminRequest{UserID: "9000", Reset: true}
	for _, actor := range []uint64{424242, modActor} {
		reply := h.admin(t, "voice_usage_set", actor, req)
		assert.Equal(t, domainrpc.CodeForbidden, reply.Code)
	}
	assert.Zero(t, h.client.VoiceUsage.Query().CountX(t.Context()))
}

func TestAdminVoiceUsageAuditsCommittedAdjustment(t *testing.T) {
	h := voiceAdminHarness(t)
	audit := serveVoiceAudit(t, h)

	percent := 25
	used := voiceplan.SecondsToTokens(voiceplan.MonthlySeconds(voiceplan.VIP)) / 4
	change := usersrpc.AdminRequest{UserID: "9000", UsedPercent: &percent}
	set := adminVoiceAllowance(t, h.admin(t, "voice_usage_set", adminActor, change))
	change.UsedPercent, change.Reset = nil, true
	reset := adminVoiceAllowance(t, h.admin(t, "voice_usage_set", adminActor, change))
	assert.Equal(t, []int64{used, 0}, []int64{set.UsedTokens, reset.UsedTokens})

	read := adminVoiceAllowance(t, h.admin(t, "get", adminActor, usersrpc.AdminRequest{UserID: "9000"}))
	assert.Equal(t, reset, read)
	assertVoiceAudit(t, audit, voiceAuditDetails{
		period: set.Period, used: used, resetPeriod: reset.Period,
	})
}

func voiceAdminHarness(t *testing.T) harness {
	t.Helper()
	h := newHarness(t)
	h.staff(staffFixture{modActor, adminauth.Moderator, true}, staffFixture{adminActor, adminauth.Admin, true})
	h.users(1, time.Now())
	h.client.User.UpdateOneID(9000).SetIsActive(true).SetStatus(user.StatusVip).ExecX(t.Context())
	return h
}

func serveVoiceAudit(t *testing.T, h harness) *adminent.Client {
	t.Helper()
	open := func(dialect, dsn string) *adminent.Client { return enttest.Open(t, dialect, dsn) }
	audit := testdb.Open(t, testdb.Name(t.Name()+"_audit"), open)
	wiring := adminrpc.Wiring{RPCWiring: bus.RPCWiring{NC: h.nc, Log: zap.NewNop()}}
	require.NoError(t, adminrpc.SubscribeAuditDelivery(wiring, audit, adminauth.AuditDeliverySubject))
	return audit
}

func adminVoiceAllowance(t *testing.T, reply usersrpc.AdminReply) usersrpc.VoiceAllowance {
	t.Helper()
	require.Empty(t, reply.Error)
	require.NotNil(t, reply.User)
	require.NotNil(t, reply.User.VoiceAllowance)
	return *reply.User.VoiceAllowance
}

type voiceAuditDetails struct {
	period      string
	used        int64
	resetPeriod string
}

func assertVoiceAudit(t *testing.T, audit *adminent.Client, details voiceAuditDetails) {
	t.Helper()
	rows := audit.AdminAudit.Query().Order(adminent.Asc(adminaudit.FieldID)).AllX(t.Context())
	require.Len(t, rows, 2)
	assert.Equal(t, []string{
		fmt.Sprintf("operation=set old_used_tokens=0 new_used_tokens=%d period=%s", details.used, details.period),
		fmt.Sprintf("operation=reset old_used_tokens=%d new_used_tokens=0 period=%s", details.used, details.resetPeriod),
	}, []string{rows[0].Detail, rows[1].Detail})
	for _, row := range rows {
		assert.Equal(t, []any{
			uint64(adminActor), fmt.Sprintf("staff-%d", adminActor), "9000", usersrpc.AdminVoiceUsageAuditAction, true,
		}, []any{row.ActorID, row.ActorLogin, row.Target, row.Action, row.Ok})
	}
}
