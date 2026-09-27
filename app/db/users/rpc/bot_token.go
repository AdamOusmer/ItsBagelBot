// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"context"
)

// The trusted admin console exchanges the OAuth code and verifies the bot identity.
// This separate RPC permits only that configured account to replace its own token.
func (a *adminRPC) botTokenSet(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	if a.botUserID == "" || req.UserID != a.botUserID || req.ActorID != a.botUserID {
		return adminError(domainrpc.Refused(domainrpc.CodeForbidden, "configured bot identity required"))
	}
	if req.AccessToken == "" || req.RefreshToken == "" {
		return adminError(domainrpc.Refused(domainrpc.CodeInvalid, "bot OAuth tokens required"))
	}
	return a.tokenSet(ctx, req)
}
