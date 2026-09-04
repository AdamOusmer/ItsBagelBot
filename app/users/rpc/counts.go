// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

// countsTimeout bounds the one repo query this verb runs.
const countsTimeout = 3 * time.Second

// SubscribeCounts exposes the narrow public enrollment counts (total and
// active user counts, nothing tiered or identifying) that a cosmetic surface
// -- discord-ingress's gateway presence today -- can call directly.
//
// This is deliberately NOT the admin stats verb (bagel.rpc.admin.user.stats):
// that surface also reports premium/paid/VIP breakdowns and is reserved for
// the console's admin-authenticated callers. Pointing an internet-facing
// process (the Discord gateway pod holds the one bot-token websocket and
// receives whatever anyone sends it) at the admin RPC would hand it more of
// the user base's makeup than a "Watching N streams" status line needs, for
// no benefit. counts.get reuses the same repository.UserStats query the admin
// stats verb calls -- one query, two narrower or wider views over it -- and
// its own NATS export/import grant (see deploy/messaging/nats-auth.conf) is
// scoped to this one subject so a compromised caller gains nothing else.
func SubscribeCounts(w Wiring, subject string) error {
	repo := w.Repo
	return bus.QueueSubscribeJSON[usersrpc.CountsRequest, usersrpc.CountsReply](
		w.NC, subject, w.Queue, countsTimeout, w.App, w.Log,
		func(ctx context.Context, _ usersrpc.CountsRequest) usersrpc.CountsReply {
			total, active, _, _, err := repo.UserStats(ctx)
			if err != nil {
				return usersrpc.CountsReply{Error: err.Error()}
			}
			return usersrpc.CountsReply{TotalUsers: total, ActiveUsers: active}
		},
	)
}
