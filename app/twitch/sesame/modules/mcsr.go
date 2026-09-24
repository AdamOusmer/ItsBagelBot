// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"go.uber.org/zap"
)

const mcsrModuleName = "mcsr"

const mcsrCooldown = 10 * time.Second

const (
	defaultMcsrEloTemplate     = "{player}: {elo} elo · rank #{rank} · {wins}W {losses}L this season"
	defaultMcsrSessionTemplate = "{player} this stream: {elochange} elo ({elo} now) · {wins}W {losses}L {draws}D in {matches} matches"

	legacyMcsrSessionTemplate = "{player} this stream: {elochange} elo ({elo} now) · {wins}W {losses}L in {matches} matches"

	defaultMcsrLastMatchTemplate = "{player} vs {opponent}: {result} · {time} · {seed} {structure} · {elochange} elo · {ago} ago"
	defaultMcsrRecordTemplate    = "{playera} {winsa} - {winsb} {playerb} · {played} played"
	defaultMcsrLbTemplate        = "{board}: {list}"
	defaultMcsrRaceTemplate      = "#1 {leader} ({leadertime}) · {player}: {time} (#{rank})"
	defaultMcsrPbTemplate        = "{player}: {time} ({window} PB)"

	defaultMcsrPaceTemplate     = "{player} this session: {nethers} nethers (avg {nether}) · bastion {bastion} · fortress {fortress} · fp {firstportal} · {nph} nph"
	defaultMcsrNethersTemplate  = "{player}: {nethers} nethers this session (avg {nether}) · {nph} nph"
	defaultMcsrLastFortTemplate = "{player} last fort: nether {nether} · bastion {bastion} · fortress {fortress} · fp {firstportal} · sh {stronghold} · {ago} ago"
)

type mcsrConfig struct {
	linkedAccountConfig

	EloEnabled     string `json:"eloEnabled"`
	EloMessage     string `json:"eloMessage"`
	SessionEnabled string `json:"sessionEnabled"`
	SessionMessage string `json:"sessionMessage"`

	PaceEnabled     string `json:"paceEnabled"`
	PaceMessage     string `json:"paceMessage"`
	NethersEnabled  string `json:"nethersEnabled"`
	NethersMessage  string `json:"nethersMessage"`
	LastFortEnabled string `json:"lastFortEnabled"`
	LastFortMessage string `json:"lastFortMessage"`

	LastMatchEnabled string `json:"lastMatchEnabled"`
	LastMatchMessage string `json:"lastMatchMessage"`
	RecordEnabled    string `json:"recordEnabled"`
	RecordMessage    string `json:"recordMessage"`
	LbEnabled        string `json:"lbEnabled"`
	LbMessage        string `json:"lbMessage"`
	RaceEnabled      string `json:"raceEnabled"`
	RaceMessage      string `json:"raceMessage"`

	PbEnabled string `json:"pbEnabled"`
	PbMessage string `json:"pbMessage"`
}

func Mcsr(d engine.Deps) module.Module {
	m := module.NewModule(mcsrModuleName, module.KindOptIn)

	m.Command("elo").Everyone().Cooldown(mcsrCooldown).Aliases("mcsr", "ranked").
		Run(mcsrEloRun(d))
	m.Command("session").Everyone().Cooldown(mcsrCooldown).Aliases("mcsrsession").
		Run(mcsrSessionRun(d))
	m.Command("lastmatch").Everyone().Cooldown(mcsrCooldown).Aliases("rankedmatch").
		Run(mcsrLastMatchRun(d))
	m.Command("record").Everyone().Cooldown(mcsrCooldown).Aliases("matchrecord").
		Run(mcsrRecordRun(d))
	m.Command("lb").Everyone().Cooldown(mcsrCooldown).Aliases("leaderboard", "rankedlb").
		Run(mcsrLbRun(d))
	m.Command("race").Everyone().Cooldown(mcsrCooldown).Aliases("weeklyrace").
		Run(mcsrRaceRun(d))
	m.Command("pb").Everyone().Cooldown(mcsrCooldown).Aliases("personalbest").
		Run(mcsrPbRun(d))
	m.Command("pace").Everyone().Cooldown(mcsrCooldown).Aliases("pacesession", "splits").
		Run(mcsrPaceRun(d))
	m.Command("nethers").Everyone().Cooldown(mcsrCooldown).Aliases("nph").
		Run(mcsrNethersRun(d))
	m.Command("lastfort").Everyone().Cooldown(mcsrCooldown).Aliases("lastpace", "previousfort").
		Run(mcsrLastFortRun(d))

	online, offline := snapshotHandlers(d, snapshotSpec[mcsrConfig, gossiprpc.McsrSnapshotReply]{
		provider: mcsrProvider,
		enabled:  func(mcsrConfig) bool { return true },
		request:  mcsrSnapshotRequest,
		stored:   func(r *gossiprpc.McsrSnapshotReply) zap.Field { return zap.Int("elo", r.Elo) },
	})
	m.On("stream.online", online)
	m.On("stream.offline", offline)

	return m.Build()
}

func mcsrSnapshotRequest(c *module.Context, cfg mcsrConfig, channelID string) gossiprpc.Request {
	account, _ := resolveLinked(c, accountSources{
		Linked: cfg.Account, LinkedUUID: cfg.AccountUUID, PreferUUID: true,
	})
	return gossiprpc.Request{Account: account, ChannelID: channelID, IsPremium: c.Regress.IsPremium()}
}

func mcsrRoute(endpoint string) engine.GossipRoute {
	return engine.GossipRoute{Provider: mcsrProvider, Endpoint: endpoint}
}

func pacemanRoute(endpoint string) engine.GossipRoute {
	return engine.GossipRoute{Provider: pacemanProvider, Endpoint: endpoint}
}

type mcsrRequest = func(statsCall[mcsrConfig], statsSubject) gossiprpc.Request

const (
	mcsrProvider    = "mcsr"
	pacemanProvider = "paceman"
)

func mcsrCommand[R any](d engine.Deps, route engine.GossipRoute, enabled func(mcsrConfig) string, render func(statsCall[mcsrConfig], *R) string) statsHandler[mcsrConfig, R] {
	return statsHandler[mcsrConfig, R]{
		d:       d,
		enabled: enabled,
		route:   route,
		target:  linkedTarget[mcsrConfig](route.Provider == mcsrProvider),
		request: accountRequest[mcsrConfig],
		render:  render,
	}
}

func mcsrSeasonRun[R any](h statsHandler[mcsrConfig, R], scope func(season int) mcsrRequest) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		rest, season := parseMcsrSeason(args)
		scoped := h
		scoped.request = scope(season)
		return scoped.run(ctx, c, rest, emit)
	}
}

func mcsrAccountSeason(season int) mcsrRequest {
	return func(call statsCall[mcsrConfig], subject statsSubject) gossiprpc.Request {
		return gossiprpc.Request{Account: subject.Account, Season: season, IsPremium: call.Ctx.Regress.IsPremium()}
	}
}

func mcsrEmpty[R any](key string, empty func(*R) (player string, isEmpty bool)) func(statsCall[mcsrConfig], *R) (string, bool) {
	return func(call statsCall[mcsrConfig], reply *R) (string, bool) {
		player, isEmpty := empty(reply)
		if !isEmpty {
			return "", false
		}
		return mcsrEmptyText(call.Ctx, player, key), true
	}
}
