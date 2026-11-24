// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/pkg/bus"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

type Safety struct {
	Store valkey.Client
	Rules pkgvalkey.SpeechSafety
}

type Wiring struct {
	bus.RPCWiring
	Repo      *repository.Users
	Safety    Safety
	Campaigns CampaignTester
}
