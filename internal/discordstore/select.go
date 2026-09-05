// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"github.com/nats-io/nats.go"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// DefaultRPCPrefix is discord-data's subject prefix (NATS_DISCORD_DATA_RPC_PREFIX).
const DefaultRPCPrefix = "bagel.rpc.discord-data"

// Selection is what a process knows at boot about where its Discord state
// should live.
type Selection struct {
	// Enabled is DISCORD_DATA_ENABLED. False keeps every keyspace in Valkey,
	// which is what the fleet ran before discord-data existed.
	Enabled bool
	// NC is the RPC connection. Nil with Enabled true is a misconfiguration,
	// not a choice, and is treated the same as Enabled false plus a louder log.
	NC     *nats.Conn
	Prefix string
	Client valkey.Client
	Log    *zap.Logger
}

// Select builds the Store a process boots with: discord-data behind a Valkey
// cache when the service is enabled, pure Valkey otherwise.
//
// The fallback logs a WARN rather than failing the boot on purpose. Pure Valkey
// is a real, working mode -- it is what shipped before discord-data existed --
// but it silently loses everything the durable half provides: ticket ids, the
// per-member open limit, transcripts, and any ticket history that outlives the
// channel. A process running that way needs to be visible in the logs, because
// the symptom (a desk that never refuses an open) looks like a config mistake
// rather than a deployment one.
func Select(sel Selection) Store {
	log := sel.Log
	if log == nil {
		log = zap.NewNop()
	}
	if !sel.Enabled {
		log.Warn("discord-data is disabled: tickets and XP stay in Valkey, with no open limit, transcripts or history")
		return New(sel.Client)
	}
	if sel.NC == nil {
		log.Warn("discord-data is enabled but no RPC connection was supplied: falling back to Valkey")
		return New(sel.Client)
	}
	return NewRPC(sel.NC, prefixOr(sel.Prefix), sel.Client, log)
}

func prefixOr(prefix string) string {
	if prefix == "" {
		return DefaultRPCPrefix
	}
	return prefix
}
