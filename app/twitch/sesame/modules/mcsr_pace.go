// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strconv"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

// This file holds the PaceMan-backed commands: !pace, !nethers and !lastfort.
// They ride the same linked account as the MCSR Ranked commands but answer
// through the paceman gossip provider — PaceMan.gg tracks live speedrun splits,
// a different upstream from MCSR Ranked's match results, with its own cache and
// rate-limit budget.
//
// All three are the plain shape externalCommand (external.go) exists for: the
// linked account, an account-only request, a template over reply-shaped tokens,
// and one translated line when the reply carries no run data at all. preferName
// is set on every one of them because PaceMan's API is keyed by username and
// rejects the Mojang uuid the module may have stored.

// mcsrPaceRun answers !pace with this session's PaceMan split averages, nether
// count and nethers-per-hour. Template tokens: {player} {nethers} {nether}
// {bastion} {fortress} {firststructure} {secondstructure} {firstportal}
// {stronghold} {end} {finish} {nph}. No nethers tracked this window is a normal
// PaceMan answer (the player simply hasn't started a run), not an error, so it
// gets a plain translated line instead of a template full of zeroes.
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

// mcsrPaceTokens resolves !pace's template tokens: {player} {nethers}
// {nether} {nph} plus the per-split averages.
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

// mcsrNethersRun answers !nethers with just the session's nether-entrance
// count and pace. Template tokens: {player} {nethers} {nether} {nph}.
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

// mcsrNethersTokens resolves !nethers' template tokens: {player} {nethers}
// {nether} {nph}.
var mcsrNethersTokens = module.TokenExpander[gossiprpc.PacemanNethersReply]{
	"player":  func(r *gossiprpc.PacemanNethersReply) string { return r.Player },
	"nethers": func(r *gossiprpc.PacemanNethersReply) string { return strconv.Itoa(r.Count) },
	"nether":  func(r *gossiprpc.PacemanNethersReply) string { return r.Avg },
	"nph":     func(r *gossiprpc.PacemanNethersReply) string { return trimScore(r.NPH) },
}

// mcsrLastFortRun answers !lastfort with the most recent run that reached a
// second structure (bastion or fortress). Template tokens: {player} {nether}
// {bastion} {fortress} {firstportal} {stronghold} {end} {finish} {ago}. An
// empty lookback window (no fortress pace recently) is a normal answer, not an
// error.
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

// mcsrLastFortTokens resolves !lastfort's template tokens: {player} {ago}
// plus the run's per-split times (dashed via mcsrSplit when a run never
// reached that split).
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
