// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"regexp"
	"strings"

	"ItsBagelBot/app/db/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

var topLoginPattern = regexp.MustCompile(`^[a-z0-9_]{1,25}$`)

func topLogin(raw string) string {
	login := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
	if topLoginPattern.MatchString(login) {
		return login
	}
	return ""
}

func searchesViewer(req loyaltyrpc.Request) bool {
	return req.ViewerID != "" || req.ViewerLogin != "" || req.Query != ""
}

func topQuery(req loyaltyrpc.Request, viewerID uint64) (repository.TopQuery, bool) {
	query := repository.TopQuery{Limit: req.Limit, Offset: req.Offset, ViewerID: viewerID, ViewerLogin: topLogin(req.ViewerLogin)}
	if viewerID == 0 && query.ViewerLogin == "" {
		return query, !searchesViewer(req)
	}
	return query, true
}

func (l *loyaltyRPC) handleTopGet(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	query, searchable := topQuery(req, viewerID)
	if !searchable {
		return loyaltyrpc.Reply{Found: true}
	}
	page, err := l.repo.TopPage(ctx, userID, query)
	if err != nil {
		return l.fail("loyalty top.get", err)
	}
	top := make([]loyaltyrpc.Balance, 0, len(page.Rows))
	for _, row := range page.Rows {
		view := balanceView(row.Balance)
		view.Rank = row.Rank
		top = append(top, *view)
	}
	return loyaltyrpc.Reply{Top: top, Total: page.Total, Found: true}
}
