// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"testing"
	"time"
)

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
	for _, tc := range cases {
		t.Run(tc.subject, func(t *testing.T) {
			if tc.server >= tc.console {
				t.Fatalf("%s: server %v must be shorter than the console's %v",
					tc.subject, tc.server, tc.console)
			}
		})
	}
}
