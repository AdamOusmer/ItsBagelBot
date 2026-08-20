// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strconv"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

// This file holds the PaceMan-backed commands: !pace, !nethers and
// !lastfort. They ride the same linked account and mcsrHandler shape as the
// MCSR Ranked commands in mcsr_ranked.go, but answer through the paceman
// gossip provider — PaceMan.gg tracks live speedrun splits, a different
// upstream from MCSR Ranked's match results. Their template-token resolvers
// build a map and hand it to mcsrTokenLookup (in mcsr.go) rather than each
// rolling its own switch/fallback dispatch.

// mcsrPaceSpec is what a pace-family command (!pace, !nethers, !lastfort)
// supplies to mcsrPaceCommand: its own toggle, endpoint, "no data yet" check
// and message/template/tokens. All three share the same account-only
// request and the same "empty reply -> one plain i18n line" shape (see
// mcsrEmptyText, in mcsr.go), so that wiring lives once in mcsrPaceCommand
// instead of once per command.
type mcsrPaceSpec[R any] struct {
	enabled  func(mcsrConfig) string
	endpoint string
	isEmpty  func(R) (player, key string, empty bool)
	message  func(mcsrConfig) string
	template string
	tokens   func(R) func(string) (string, bool)
}

// mcsrPaceCommand builds the mcsrHandler shared by every PaceMan-backed
// command.
func mcsrPaceCommand[R any](d engine.Deps, spec mcsrPaceSpec[R]) mcsrHandler[R] {
	return mcsrHandler[R]{
		d:       d,
		enabled: spec.enabled,
		route:   engine.GossipRoute{Provider: "paceman", Endpoint: spec.endpoint},
		request: mcsrSimpleRequest,
		reply: func(c *module.Context, cfg mcsrConfig, reply R) string {
			if player, key, empty := spec.isEmpty(reply); empty {
				return mcsrEmptyText(c, player, key)
			}
			tmpl := orDefault(spec.message(cfg), spec.template)
			return module.ExpandString(tmpl, spec.tokens(reply))
		},
	}
}

// mcsrPaceRun answers !pace with this session's PaceMan split averages,
// nether count and nethers-per-hour. Template tokens: {player} {nethers}
// {nether} {bastion} {fortress} {firstportal} {stronghold} {end} {finish}
// {nph}. No nethers tracked this window is a normal PaceMan answer (the
// player simply hasn't started a run), not an error, so it gets a plain
// i18n line instead of a template full of zeroes. See mcsrPaceCommands
// below for its wiring.

// mcsrPaceTokens resolves !pace's template tokens: {player} {nethers}
// {nether} {nph} plus the per-split averages.
func mcsrPaceTokens(reply gossiprpc.PacemanSessionReply) func(string) (string, bool) {
	tokens := map[string]string{
		"player":          reply.Player,
		"nethers":         strconv.Itoa(reply.NetherCount),
		"nether":          reply.Nether,
		"nph":             trimScore(reply.NPH),
		"bastion":         reply.Bastion,
		"fortress":        reply.Fortress,
		"firststructure":  reply.FirstStructure,
		"secondstructure": reply.SecondStructure,
		"firstportal":     reply.FirstPortal,
		"stronghold":      reply.Stronghold,
		"end":             reply.End,
		"finish":          reply.Finish,
	}
	return func(key string) (string, bool) { return mcsrTokenLookup(tokens, key) }
}

// !nethers answers with just the session's nether-entrance count and pace.
// Template tokens: {player} {nethers} {nether} {nph}. See mcsrPaceCommands
// below for its wiring.

// mcsrNethersTokens resolves !nethers' template tokens: {player} {nethers}
// {nether} {nph}.
func mcsrNethersTokens(reply gossiprpc.PacemanNethersReply) func(string) (string, bool) {
	tokens := map[string]string{
		"player":  reply.Player,
		"nethers": strconv.Itoa(reply.Count),
		"nether":  reply.Avg,
		"nph":     trimScore(reply.NPH),
	}
	return func(key string) (string, bool) { return mcsrTokenLookup(tokens, key) }
}

// mcsrLastFortRun answers !lastfort with the most recent run that reached a
// second structure (bastion or fortress). Template tokens: {player} {nether}
// {bastion} {fortress} {firstportal} {stronghold} {ago}. An empty lookback
// window (no fortress pace recently) is a normal answer, not an error.
// See mcsrPaceCommands below for its wiring.

// mcsrLastFortTokens resolves !lastfort's template tokens: {player} {ago}
// plus the run's per-split times (dashed via mcsrSplit when a run never
// reached that split).
func mcsrLastFortTokens(reply gossiprpc.PacemanLastFortReply) func(string) (string, bool) {
	tokens := map[string]string{
		"player":      reply.Player,
		"ago":         mcsrAge(reply.AgoSeconds),
		"nether":      mcsrSplit(reply.Nether),
		"bastion":     mcsrSplit(reply.Bastion),
		"fortress":    mcsrSplit(reply.Fortress),
		"firstportal": mcsrSplit(reply.FirstPortal),
		"stronghold":  mcsrSplit(reply.Stronghold),
		"end":         mcsrSplit(reply.End),
		"finish":      mcsrSplit(reply.Finish),
	}
	return func(key string) (string, bool) { return mcsrTokenLookup(tokens, key) }
}

// mcsrPaceCommands builds the PaceMan-backed command family's wiring from
// one table: !pace, !nethers and !lastfort all share mcsrPaceCommand's
// shape and differ only in these fields.
func mcsrPaceCommands(d engine.Deps) []mcsrCommandReg {
	return []mcsrCommandReg{
		{
			name:    "pace",
			aliases: []string{"pacesession", "splits"},
			run: mcsrPaceCommand(d, mcsrPaceSpec[gossiprpc.PacemanSessionReply]{
				enabled:  func(cfg mcsrConfig) string { return cfg.PaceEnabled },
				endpoint: "session",
				isEmpty: func(r gossiprpc.PacemanSessionReply) (string, string, bool) {
					return r.Player, "mcsr.pace.empty", r.Empty
				},
				message:  func(cfg mcsrConfig) string { return cfg.PaceMessage },
				template: defaultMcsrPaceTemplate,
				tokens:   mcsrPaceTokens,
			}).run,
		},
		{
			name:    "nethers",
			aliases: []string{"nph"},
			run: mcsrPaceCommand(d, mcsrPaceSpec[gossiprpc.PacemanNethersReply]{
				enabled:  func(cfg mcsrConfig) string { return cfg.NethersEnabled },
				endpoint: "nethers",
				isEmpty: func(r gossiprpc.PacemanNethersReply) (string, string, bool) {
					return r.Player, "mcsr.pace.empty", r.Empty
				},
				message:  func(cfg mcsrConfig) string { return cfg.NethersMessage },
				template: defaultMcsrNethersTemplate,
				tokens:   mcsrNethersTokens,
			}).run,
		},
		{
			name:    "lastfort",
			aliases: []string{"lastpace", "previousfort"},
			run: mcsrPaceCommand(d, mcsrPaceSpec[gossiprpc.PacemanLastFortReply]{
				enabled:  func(cfg mcsrConfig) string { return cfg.LastFortEnabled },
				endpoint: "lastfort",
				isEmpty: func(r gossiprpc.PacemanLastFortReply) (string, string, bool) {
					return r.Player, "mcsr.pace.nofort", r.Empty
				},
				message:  func(cfg mcsrConfig) string { return cfg.LastFortMessage },
				template: defaultMcsrLastFortTemplate,
				tokens:   mcsrLastFortTokens,
			}).run,
		},
	}
}
