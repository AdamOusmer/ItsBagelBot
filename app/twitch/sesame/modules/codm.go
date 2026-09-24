// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

const (
	codmModuleName = "codm"
	codmCooldown   = 10 * time.Second
	codmUsage      = "Usage: !codm [UID or exact nickname]"
	codmTemplate   = "{player} · level {level} · MP {rank} · {rating} rating · {country}"
)

type codmConfig struct {
	linkedAccountConfig

	ProfileEnabled string `json:"profileEnabled"`
	ProfileMessage string `json:"profileMessage"`
}

func CODM(d engine.Deps) module.Module {
	tokens := codmProfileTokens()
	profile := externalCommand[codmConfig, gossiprpc.CODMProfileReply]{
		route:    engine.GossipRoute{Provider: codmModuleName, Endpoint: "profile"},
		enabled:  func(c codmConfig) string { return c.ProfileEnabled },
		message:  func(c codmConfig) string { return c.ProfileMessage },
		fallback: codmTemplate,
		tokens:   tokens,
	}.handler(d)
	profile.target = codmProfileTarget
	profile.render = func(call statsCall[codmConfig], reply *gossiprpc.CODMProfileReply) string {
		safeReply := *reply
		safeReply.Player = codmProfileTarget(call).Display
		return tokens.Expand(orDefault(call.Cfg.ProfileMessage, codmTemplate), &safeReply)
	}

	m := module.NewModule(codmModuleName, module.KindOptIn)
	m.Command(codmModuleName).Everyone().Cooldown(codmCooldown).
		Aliases("codmprofile", "codmrank").Run(profile.run)
	return m.Build()
}

func codmProfileTarget(call statsCall[codmConfig]) statsSubject {
	account := strings.TrimSpace(call.Args)
	linked := strings.TrimSpace(call.Cfg.Account)

	if account == "" || explicitOn(call.Cfg.LinkedOnly) {
		account = linked
	}
	if account == "" {
		return statsSubject{Refusal: codmUsage, Display: codmUsage}
	}
	return statsSubject{Account: account, Display: account}
}

func codmProfileTokens() module.TokenExpander[gossiprpc.CODMProfileReply] {
	type reply = gossiprpc.CODMProfileReply
	return module.TokenExpander[reply]{
		"player":    func(r *reply) string { return r.Player },
		"level":     func(r *reply) string { return strconv.Itoa(r.Level) },
		"rank":      func(r *reply) string { return r.Rank },
		"rankclass": func(r *reply) string { return strconv.Itoa(r.RankClass) },
		"rating":    func(r *reply) string { return strconv.Itoa(r.Rating) },
		"country":   func(r *reply) string { return r.Country },
		"shortid":   func(r *reply) string { return r.ShortID },
	}
}
