// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"ItsBagelBot/app/db/users/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

func (a *adminRPC) setVoiceUsage(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	id, err := bus.UserID(req.UserID)
	if err != nil {
		return adminError(refusal(err))
	}
	u, err := a.repo.FindUser(ctx, id)
	if err != nil {
		return adminError(refusal(err))
	}
	change := repository.AdminVoiceUsageChange{
		VoiceUsageChange: repository.VoiceUsageChange{Reset: req.Reset, UsedPercent: req.UsedPercent},
		ActorID:          req.ActorID,
	}
	allowance, err := a.repo.AdjustAdminVoiceUsage(ctx, id, change, time.Now())
	if err != nil {
		return adminError(bus.Classify(err,
			domainrpc.Is(repository.ErrInvalidVoiceUsageChange, domainrpc.CodeInvalid),
			domainrpc.Is(repository.ErrNoVoiceAllowance, domainrpc.CodeInvalid),
			domainrpc.Is(repository.ErrVoiceUsageForbidden, domainrpc.CodeForbidden),
			entNotFound,
		))
	}
	view := viewOf(u)
	view.VoiceAllowance = &allowance
	return usersrpc.AdminReply{User: &view}
}
