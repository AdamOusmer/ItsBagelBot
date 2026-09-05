// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

// DefaultRPCPrefix is discord-data's subject prefix (NATS_DISCORD_DATA_RPC_PREFIX).
//
// Integration note (2026-09-05): this file used to export Select/Selection and
// a DISCORD_DATA_ENABLED switch that let engine and outgress boot against pure
// Valkey when discord-data was unreachable. That fallback was removed when the
// multi-guild model landed, not because it was untidy but because it can no
// longer work: per-guild configuration lives in discord-data's guild_configs
// table (contract H), so a process in "Valkey only" mode resolves every guild
// to an empty Config and runs as a bot that is connected, healthy, and silently
// does nothing. The old mode was safe when Valkey still held the bindings; it
// stopped being safe the moment config.get became the only source of settings.
// Both mains now call NewRPC unconditionally and let writes fail loudly.
const DefaultRPCPrefix = "bagel.rpc.discord-data"
