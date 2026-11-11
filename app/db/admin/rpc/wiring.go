// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"time"

	"ItsBagelBot/app/db/admin/ent"
	"ItsBagelBot/app/db/admin/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/bus"
)

const adminBudget = 3 * time.Second

type Wiring struct {
	bus.RPCWiring
	Repo *repository.Admin
}

func refusal(err error) domainrpc.Refusal {
	return bus.Classify(err, domainrpc.When(ent.IsNotFound, domainrpc.CodeNotFound), domainrpc.Is(errAuditOperationConflict, domainrpc.CodeInvalid))
}
