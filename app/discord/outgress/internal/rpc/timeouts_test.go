// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"testing"
	"time"
)

// consoleDeadlines are the dashboard's own client timeouts, copied by hand
// from web/dashboard/src/lib/server/discord-store.ts (SETUP_TIMEOUT_MS,
// LAYOUT_TIMEOUT_MS, STATUS_TIMEOUT_MS, REPOST_TIMEOUT_MS, UNBIND_TIMEOUT_MS,
// CONFIG_GET_TIMEOUT_MS, CONFIG_SET_TIMEOUT_MS, GUILDS_TIMEOUT_MS).
//
// Hardcoded because there is nothing to import: the console is TypeScript, and
// the two numbers meet only over NATS. Copying them is what makes the pairing
// checkable at all -- change one of these in the console and this test is the
// thing that notices, which is worth more than the copy costs. Update this
// table in the same commit as the TS file.
const (
	consoleSetupMS     = 60000
	consoleLayoutMS    = 12000
	consoleStatusMS    = 3000
	consoleRepostMS    = 10000
	consoleUnbindMS    = 5000
	consoleConfigGetMS = 2000
	consoleConfigSetMS = 5000
	consoleGuildsMS    = 8000
)

// TestEveryDashboardHandlerGivesUpBeforeTheConsoleDoes pins the ordering the
// error contract depends on.
//
// The server must be the one to give up first. When it is, the dashboard gets
// a reply carrying a code (discord_unavailable, rate_limited, ...) and can say
// something true; when the console gives up first, the page shows a generic
// network failure, the handler keeps burning a Discord rate-limit slot on an
// answer nobody will read, and a retry stacks a second call on top of the
// first. Equal is not good enough either: the reply still has to travel.
func TestEveryDashboardHandlerGivesUpBeforeTheConsoleDoes(t *testing.T) {
	cases := []struct {
		subject string
		server  time.Duration
		console time.Duration
	}{
		{"discord.setup", setupHandleTimeout, consoleSetupMS * time.Millisecond},
		{"discord.layout", layoutHandleTimeout, consoleLayoutMS * time.Millisecond},
		{"discord.status", statusHandleTimeout, consoleStatusMS * time.Millisecond},
		{"discord.desk.repost", deskRepostTimeout, consoleRepostMS * time.Millisecond},
		{"discord.unbind", handleTimeout, consoleUnbindMS * time.Millisecond},
		{"discord.config.get", configGetHandleTimeout, consoleConfigGetMS * time.Millisecond},
		{"discord.config.set", configHandleTimeout, consoleConfigSetMS * time.Millisecond},
		{"discord.guilds.list", guildsHandleTimeout, consoleGuildsMS * time.Millisecond},
	}
	// discord.post is deliberately absent: it is the operator post path, not a
	// dashboard call, and the console sets no deadline for it.
	for _, tc := range cases {
		t.Run(tc.subject, func(t *testing.T) {
			if tc.server >= tc.console {
				t.Fatalf("%s: server %v must be shorter than the console's %v",
					tc.subject, tc.server, tc.console)
			}
		})
	}
}
