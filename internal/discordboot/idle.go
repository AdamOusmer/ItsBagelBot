// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package discordboot holds the boot decisions the Discord service mains make
// identically. It sits beside pkg/svcboot rather than inside it because the
// question it answers -- "is there a bot token to work with?" -- is Discord's,
// not every service's, and svcboot deliberately knows nothing about a
// particular product's configuration.
package discordboot

import (
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/svcboot"
)

// Service is the process the idle path may park: who it answers as, the token
// that decides whether it has work, and where health listens.
//
// A struct rather than three arguments because all three are strings and the
// call site would otherwise be three positional strings nothing but their
// order distinguishes.
type Service struct {
	Name   string
	Token  string
	Listen string
}

// IdleIfNoToken parks the process when no bot token is configured, and reports
// whether it did so the caller can return.
//
// Idle means: serve health, then block on the same signal context every other
// service exits on. It is NOT a fatal, and that is the whole point -- a deploy
// that deliberately leaves DISCORD_BOT_TOKEN unset (a preview namespace, a
// cluster brought up before the secret lands) gets a pod that stays Ready and
// says why, instead of a CrashLoopBackOff that reads like an outage.
//
// Both Discord mains had this same five-line block, and both must keep it: a
// gateway Identify and a REST client are equally impossible without the token.
func IdleIfNoToken(core svcboot.Core, svc Service) bool {
	if svc.Token != "" {
		return false
	}
	core.Log.Info("DISCORD_BOT_TOKEN unset; " + svc.Name + " idle")
	health.Serve(svc.Listen, svc.Name)
	core.Await()
	return true
}
