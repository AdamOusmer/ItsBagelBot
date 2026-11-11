// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"testing"

	"ItsBagelBot/app/db/admin/ent/adminaudit"
	"ItsBagelBot/app/db/admin/ent/adminuser"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func auditRequest() usersrpc.AuthRequest {
	return usersrpc.AuthRequest{AuditID: uuid.NewString(), ActorID: "101", ActorLogin: "operator", Action: "set_test_account", Target: "202", Detail: "enabled=true", OK: true}
}

func TestAuditAppendIdempotenceAndLegacyNullIDs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ids   []string
		rows  int
		nulls int
	}{
		{"same operation retries once", []string{"d648b882-2bdf-4658-b977-166d30daf513", "d648b882-2bdf-4658-b977-166d30daf513"}, 1, 0},
		{"different operations remain distinct", []string{uuid.NewString(), uuid.NewString()}, 2, 0},
		{"legacy appends remain distinct", []string{"", ""}, 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.staff(staffFixture{101, adminuser.RoleAdmin, true})
			for _, id := range tc.ids {
				req := auditRequest()
				req.AuditID = id
				require.Empty(t, h.auth(t, "audit:append", req).Error)
			}
			require.Equal(t, tc.rows, h.client.AdminAudit.Query().CountX(t.Context()))
			require.Equal(t, tc.nulls, h.client.AdminAudit.Query().Where(adminaudit.OperationIDIsNil()).CountX(t.Context()))
		})
	}
}

func TestAuditAppendRejectsConflictingOperationContents(t *testing.T) {
	h := newHarness(t)
	h.staff(staffFixture{101, adminuser.RoleAdmin, true})
	req := auditRequest()
	require.Empty(t, h.auth(t, "audit:append", req).Error)

	req.Detail = "enabled=false"
	reply := h.auth(t, "audit:append", req)
	require.Equal(t, domainrpc.CodeInvalid, reply.Code)
	require.Equal(t, 1, h.client.AdminAudit.Query().CountX(t.Context()))
}

func TestAuditDeliverySurvivesRevokedActor(t *testing.T) {
	h := newHarness(t)
	h.staff(staffFixture{101, adminuser.RoleAdmin, false})
	req := auditRequest()
	require.Equal(t, domainrpc.CodeForbidden, h.auth(t, "audit:append", req).Code)

	for range 2 {
		reply := call[usersrpc.AuthReply](t, h, auditDeliverySubject, req)
		require.Empty(t, reply.Error)
	}
	require.Equal(t, 1, h.client.AdminAudit.Query().CountX(t.Context()))
}

func TestAuditDeliveryRejectsUnrecognizedPayloads(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*usersrpc.AuthRequest)
	}{
		{"missing operation", func(r *usersrpc.AuthRequest) { r.AuditID = "" }},
		{"invalid target", func(r *usersrpc.AuthRequest) { r.Target = "unknown" }},
		{"unrelated action", func(r *usersrpc.AuthRequest) { r.Action = "set_status" }},
		{"invalid detail", func(r *usersrpc.AuthRequest) { r.Detail = "unrecognized" }},
		{"invalid actor", func(r *usersrpc.AuthRequest) { r.ActorID = "unknown" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			req := auditRequest()
			tc.change(&req)
			reply := call[usersrpc.AuthReply](t, h, auditDeliverySubject, req)
			require.Equal(t, domainrpc.CodeInvalid, reply.Code)
			require.Zero(t, h.client.AdminAudit.Query().CountX(t.Context()))
		})
	}
}
