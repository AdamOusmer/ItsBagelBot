// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strconv"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

func mcsrPaceRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.PacemanSessionReply
	return externalCommand[mcsrConfig, reply]{
		route:      pacemanRoute("session"),
		enabled:    func(cfg mcsrConfig) string { return cfg.PaceEnabled },
		message:    func(cfg mcsrConfig) string { return cfg.PaceMessage },
		fallback:   defaultMcsrPaceTemplate,
		tokens:     mcsrPaceTokens,
		special:    mcsrEmpty("mcsr.pace.empty", func(r *reply) (string, bool) { return r.Player, r.Empty }),
		preferName: true,
	}.run(d)
}

var mcsrPaceTokens = module.TokenExpander[gossiprpc.PacemanSessionReply]{
	"player":          func(r *gossiprpc.PacemanSessionReply) string { return r.Player },
	"nethers":         func(r *gossiprpc.PacemanSessionReply) string { return strconv.Itoa(r.NetherCount) },
	"nether":          func(r *gossiprpc.PacemanSessionReply) string { return r.Nether },
	"nph":             func(r *gossiprpc.PacemanSessionReply) string { return trimScore(r.NPH) },
	"bastion":         func(r *gossiprpc.PacemanSessionReply) string { return r.Bastion },
	"fortress":        func(r *gossiprpc.PacemanSessionReply) string { return r.Fortress },
	"firststructure":  func(r *gossiprpc.PacemanSessionReply) string { return r.FirstStructure },
	"secondstructure": func(r *gossiprpc.PacemanSessionReply) string { return r.SecondStructure },
	"firstportal":     func(r *gossiprpc.PacemanSessionReply) string { return r.FirstPortal },
	"stronghold":      func(r *gossiprpc.PacemanSessionReply) string { return r.Stronghold },
	"end":             func(r *gossiprpc.PacemanSessionReply) string { return r.End },
	"finish":          func(r *gossiprpc.PacemanSessionReply) string { return r.Finish },
}

func mcsrNethersRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.PacemanNethersReply
	return externalCommand[mcsrConfig, reply]{
		route:      pacemanRoute("nethers"),
		enabled:    func(cfg mcsrConfig) string { return cfg.NethersEnabled },
		message:    func(cfg mcsrConfig) string { return cfg.NethersMessage },
		fallback:   defaultMcsrNethersTemplate,
		tokens:     mcsrNethersTokens,
		special:    mcsrEmpty("mcsr.pace.empty", func(r *reply) (string, bool) { return r.Player, r.Empty }),
		preferName: true,
	}.run(d)
}

var mcsrNethersTokens = module.TokenExpander[gossiprpc.PacemanNethersReply]{
	"player":  func(r *gossiprpc.PacemanNethersReply) string { return r.Player },
	"nethers": func(r *gossiprpc.PacemanNethersReply) string { return strconv.Itoa(r.Count) },
	"nether":  func(r *gossiprpc.PacemanNethersReply) string { return r.Avg },
	"nph":     func(r *gossiprpc.PacemanNethersReply) string { return trimScore(r.NPH) },
}

func mcsrLastFortRun(d engine.Deps) module.RunFunc {
	type reply = gossiprpc.PacemanLastFortReply
	return externalCommand[mcsrConfig, reply]{
		route:      pacemanRoute("lastfort"),
		enabled:    func(cfg mcsrConfig) string { return cfg.LastFortEnabled },
		message:    func(cfg mcsrConfig) string { return cfg.LastFortMessage },
		fallback:   defaultMcsrLastFortTemplate,
		tokens:     mcsrLastFortTokens,
		special:    mcsrEmpty("mcsr.pace.nofort", func(r *reply) (string, bool) { return r.Player, r.Empty }),
		preferName: true,
	}.run(d)
}

var mcsrLastFortTokens = module.TokenExpander[gossiprpc.PacemanLastFortReply]{
	"player":      func(r *gossiprpc.PacemanLastFortReply) string { return r.Player },
	"ago":         func(r *gossiprpc.PacemanLastFortReply) string { return mcsrAge(r.AgoSeconds) },
	"nether":      func(r *gossiprpc.PacemanLastFortReply) string { return mcsrSplit(r.Nether) },
	"bastion":     func(r *gossiprpc.PacemanLastFortReply) string { return mcsrSplit(r.Bastion) },
	"fortress":    func(r *gossiprpc.PacemanLastFortReply) string { return mcsrSplit(r.Fortress) },
	"firstportal": func(r *gossiprpc.PacemanLastFortReply) string { return mcsrSplit(r.FirstPortal) },
	"stronghold":  func(r *gossiprpc.PacemanLastFortReply) string { return mcsrSplit(r.Stronghold) },
	"end":         func(r *gossiprpc.PacemanLastFortReply) string { return mcsrSplit(r.End) },
	"finish":      func(r *gossiprpc.PacemanLastFortReply) string { return mcsrSplit(r.Finish) },
}
