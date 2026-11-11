// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"ItsBagelBot/app/db/admin/ent"
	"ItsBagelBot/app/db/admin/ent/adminaudit"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	dbgate "ItsBagelBot/pkg/db"

	"github.com/google/uuid"
)

var errAuditOperationConflict = errors.New("audit operation id reused with different content")

type auditContent struct {
	actorID uint64
	login   string
	action  string
	target  string
	detail  string
	ok      bool
	failure string
}

func (a *adminAuthRPC) auditAppend(ctx context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
	if _, errMsg := a.resolveActiveActor(ctx, req.ActorID); errMsg != "" {
		return authError(domainrpc.Refused(domainrpc.CodeForbidden, errMsg))
	}
	return a.appendAudit(ctx, req)
}

func SubscribeAuditDelivery(w Wiring, client *ent.Client, subject string) error {
	a := &adminAuthRPC{staffGate: staffGate{db: client}, log: w.Log}
	return bus.Serve(w.Within(adminBudget), subject, a.auditDeliver)
}

func (a *adminAuthRPC) auditDeliver(ctx context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
	if err := validateAuditDelivery(req); err != nil {
		return authError(domainrpc.Refused(domainrpc.CodeInvalid, err.Error()))
	}
	return a.appendAudit(ctx, req)
}

func validateAuditDelivery(req usersrpc.AuthRequest) error {
	if _, err := uuid.Parse(req.AuditID); err != nil {
		return fmt.Errorf("audit_id must be a UUID")
	}
	if _, err := parseID(req.Target); err != nil {
		return fmt.Errorf("target must be a user id")
	}
	if !validTestAccountAudit(req) {
		return fmt.Errorf("successful set_test_account action required")
	}
	if !slices.Contains([]string{"enabled=true", "enabled=false"}, req.Detail) {
		return fmt.Errorf("test-account detail required")
	}
	return nil
}

func validTestAccountAudit(req usersrpc.AuthRequest) bool {
	return req.Action == "set_test_account" && req.OK && req.Err == ""
}

func (a *adminAuthRPC) appendAudit(ctx context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
	actorID, err := parseID(req.ActorID)
	if err != nil {
		return authError(domainrpc.Refused(domainrpc.CodeInvalid, "actor_id: "+err.Error()))
	}
	if req.ActorLogin == "" || req.Action == "" {
		return authError(domainrpc.Refused(domainrpc.CodeInvalid, "actor_login and action required"))
	}
	content := auditContent{actorID: actorID, login: req.ActorLogin, action: req.Action, target: req.Target,
		detail: req.Detail, ok: req.OK, failure: req.Err}
	if err := a.saveAudit(ctx, content, req.AuditID); err != nil {
		return authError(refusal(err))
	}
	return usersrpc.AuthReply{}
}

func (a *adminAuthRPC) saveAudit(ctx context.Context, content auditContent, operationID string) error {
	create := a.db.AdminAudit.Create().SetActorID(content.actorID).SetActorLogin(content.login).
		SetAction(content.action).SetTarget(content.target).SetDetail(content.detail).SetOk(content.ok).SetError(content.failure)
	if operationID != "" {
		create.SetOperationID(operationID)
	}
	err := dbgate.WithExec(ctx, create.Exec)
	if !ent.IsConstraintError(err) || operationID == "" {
		return err
	}
	return a.matchAudit(ctx, content, operationID)
}

func (a *adminAuthRPC) matchAudit(ctx context.Context, content auditContent, operationID string) error {
	row, err := dbgate.WithRead(ctx, func(ctx context.Context) (*ent.AdminAudit, error) {
		return a.db.AdminAudit.Query().Where(adminaudit.OperationIDEQ(operationID)).Only(ctx)
	})
	if err != nil {
		return err
	}
	persisted := auditContent{actorID: row.ActorID, login: row.ActorLogin, action: row.Action, target: row.Target,
		detail: row.Detail, ok: row.Ok, failure: row.Error}
	if content != persisted {
		return errAuditOperationConflict
	}
	return nil
}
