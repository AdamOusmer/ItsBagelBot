// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strings"
	"time"

	"ItsBagelBot/app/db/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/domain/validate"
)

func topLogin(raw string) string {
	login := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
	if validate.Login(login) == nil {
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
	route, err := l.resolveBoard(ctx, userID, req)
	if err != nil {
		return l.fail("loyalty top.get", err)
	}
	query, searchable := topQuery(req, viewerID)
	page, err := route.page(ctx, query, searchable)
	if err != nil {
		return l.fail("loyalty top.get", err)
	}
	stamp, err := route.stamp(ctx)
	if err != nil {
		return l.fail("loyalty top.get", err)
	}
	return topReply(page, stamp)
}

func topReply(page repository.TopPage, stamp loyaltyrpc.FreezeStamp) loyaltyrpc.Reply {
	top := make([]loyaltyrpc.Balance, 0, len(page.Rows))
	for _, row := range page.Rows {
		view := balanceView(row.Balance)
		view.Rank = row.Rank
		top = append(top, *view)
	}
	return loyaltyrpc.Reply{Top: top, Total: page.Total, Found: true, FreezeStamp: stamp}
}

type boardRoute struct {
	repo   *repository.Loyalty
	userID uint64
	state  repository.FreezeState
	pin    repository.SnapshotID
}

func (l *loyaltyRPC) resolveBoard(ctx context.Context, userID uint64, req loyaltyrpc.Request) (boardRoute, error) {
	route := boardRoute{repo: l.repo, userID: userID}
	switch {
	case req.Live:
		return route, nil
	case req.FreezeID != "":
		route.pin = repository.SnapshotID(req.FreezeID)
		return route, nil
	default:
		state, err := l.repo.FreezeStatus(ctx, userID)
		if state.Active(time.Now()) {
			route.state = state
		}
		return route, err
	}
}

func (b boardRoute) stamp(ctx context.Context) (loyaltyrpc.FreezeStamp, error) {
	if b.pin == "" {
		return b.activeStamp(), nil
	}
	state, err := b.repo.FreezeEpoch(ctx, b.userID, b.pin)
	if err != nil {
		return loyaltyrpc.FreezeStamp{}, err
	}
	return freezeStamp(state), nil
}

func (b boardRoute) activeStamp() loyaltyrpc.FreezeStamp {
	if b.state.SnapshotID == "" {
		return loyaltyrpc.FreezeStamp{}
	}
	return freezeStamp(b.state)
}

func (b boardRoute) page(ctx context.Context, query repository.TopQuery, searchable bool) (repository.TopPage, error) {
	if !searchable {
		return repository.TopPage{}, nil
	}
	id := b.pin
	if id == "" {
		id = b.state.SnapshotID
	}
	if id == "" {
		return b.repo.TopPage(ctx, b.userID, query)
	}
	return b.repo.FrozenTopPage(ctx, b.userID, id, query)
}

func freezeStamp(state repository.FreezeState) loyaltyrpc.FreezeStamp {
	return loyaltyrpc.FreezeStamp{FreezeID: string(state.SnapshotID), FrozenAsOf: state.AsOf.UnixMilli(), ExpiresAt: state.ExpiresAt.UnixMilli()}
}
