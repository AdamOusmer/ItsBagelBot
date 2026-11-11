// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/app/db/admin/ent"
	"ItsBagelBot/app/db/admin/ent/adminaudit"
	"ItsBagelBot/app/db/admin/ent/predicate"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	dbgate "ItsBagelBot/pkg/db"
	"context"
	"strconv"
	"strings"
)

const (
	auditPageSize     = 15
	auditMaxPages     = 25
	auditMaxSearchLen = 200
)

func (a *adminAuthRPC) auditList(ctx context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
	q := a.db.AdminAudit.Query().Order(ent.Desc(adminaudit.FieldCreatedAt), ent.Desc(adminaudit.FieldID))
	if req.ActorFilter != "" {
		aid, err := parseID(req.ActorFilter)
		if err != nil {
			return authError(domainrpc.Refused(domainrpc.CodeInvalid, "actor_filter: "+err.Error()))
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

func (a *adminAuthRPC) auditListAll(ctx context.Context, q *ent.AdminAuditQuery, limit int) usersrpc.AuthReply {
	rows, err := dbgate.WithQuery(ctx, func(ctx context.Context) ([]*ent.AdminAudit, error) {
		return q.Limit(auditListLimit(limit)).All(ctx)
	})
	if err != nil {
		return authError(refusal(err))
	}
	return usersrpc.AuthReply{Entries: auditViewsOf(rows)}
}

func (a *adminAuthRPC) auditListPage(ctx context.Context, q *ent.AdminAuditQuery, page, limit int) usersrpc.AuthReply {
	p := domainrpc.ClampPage(page, auditListLimit(limit), auditPageSize, auditMaxPages)
	rows, err := dbgate.WithQuery(ctx, func(ctx context.Context) ([]*ent.AdminAudit, error) {
		return q.Offset(p.Offset()).Limit(p.FetchLimit()).All(ctx)
	})
	if err != nil {
		return authError(refusal(err))
	}
	rows, hasMore := domainrpc.PageRows(p, rows)
	return usersrpc.AuthReply{
		Entries:  auditViewsOf(rows),
		Page:     p.Number,
		PageSize: p.Size,
		MaxPages: auditMaxPages,
		HasMore:  hasMore,
	}
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
