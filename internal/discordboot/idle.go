// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordboot

import (
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/svcboot"
)

type Service struct {
	Name   string
	Token  string
	Listen string
}

func IdleIfNoToken(core svcboot.Core, svc Service) bool {
	if svc.Token != "" {
		return false
	}
	core.Log.Info("DISCORD_BOT_TOKEN unset; " + svc.Name + " idle")
	health.Serve(svc.Listen, svc.Name)
	core.Await()
	return true
}
