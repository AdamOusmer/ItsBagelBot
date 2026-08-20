// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package paceman

import (
	"context"
	"strings"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

// This file holds paceman's four gossip endpoints (session, nethers,
// lastfort, personal_best) and their error-reply helpers. The reply-shaping
// helpers built on their successful-fetch path live in paceman_reply.go.

func (p *api) session(ctx context.Context, req gossiprpc.Request) any {
	log := monitor.TxnLogger(ctx, p.log)
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gossiprpc.PacemanSessionReply{Error: "missing account"}
	}
	hoursBetween := resolveHoursBetween(req.HoursBetween)

	stats, err := p.cachedSessionStats(ctx, sessionQuery{Account: account, HoursBetween: hoursBetween}, req.IsPremium)
	if err != nil {
		return sessionErrorReply(log, account, err)
	}
	nethers, err := p.cachedSessionNethers(ctx, sessionQuery{Account: account, HoursBetween: hoursBetween}, req.IsPremium)
	if err != nil {
		return sessionErrorReply(log, account, err)
	}
	return buildSessionReply(account, stats, nethers)
}

// sessionErrorReply maps a fetch failure to a paceman.session reply: a
// friendly hit (upstream 4xx/429) stays quiet in the logs since it is normal
// upstream behavior, anything else logs a warning and answers a generic
// message so the viewer still gets a line instead of silence.
func sessionErrorReply(log *zap.Logger, account string, err error) gossiprpc.PacemanSessionReply {
	msg := friendlyError(err)
	if msg == "" {
		log.Warn("paceman session fetch failed", zap.String("account", account), zap.Error(err))
		msg = "stats lookup failed"
	}
	return gossiprpc.PacemanSessionReply{Player: account, Error: msg}
}

func (p *api) nethers(ctx context.Context, req gossiprpc.Request) any {
	log := monitor.TxnLogger(ctx, p.log)
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gossiprpc.PacemanNethersReply{Error: "missing account"}
	}
	hoursBetween := resolveHoursBetween(req.HoursBetween)

	nethers, err := p.cachedSessionNethers(ctx, sessionQuery{Account: account, HoursBetween: hoursBetween}, req.IsPremium)
	if err != nil {
		return nethersErrorReply(log, account, err)
	}
	return gossiprpc.PacemanNethersReply{
		Player: account,
		Count:  nethers.Count,
		Avg:    nethers.Avg,
		NPH:    nethers.RNPH,
		Empty:  nethers.Count == 0,
	}
}

func nethersErrorReply(log *zap.Logger, account string, err error) gossiprpc.PacemanNethersReply {
	msg := friendlyError(err)
	if msg == "" {
		log.Warn("paceman nethers fetch failed", zap.String("account", account), zap.Error(err))
		msg = "stats lookup failed"
	}
	return gossiprpc.PacemanNethersReply{Player: account, Error: msg}
}

func (p *api) lastfort(ctx context.Context, req gossiprpc.Request) any {
	log := monitor.TxnLogger(ctx, p.log)
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gossiprpc.PacemanLastFortReply{Error: "missing account"}
	}

	runs, err := p.cachedLastFort(ctx, account, req.IsPremium)
	if err != nil {
		return lastFortErrorReply(log, account, err)
	}
	if len(runs) == 0 {
		return gossiprpc.PacemanLastFortReply{Player: account, Empty: true}
	}
	return buildLastFortReply(account, runs[0])
}

func lastFortErrorReply(log *zap.Logger, account string, err error) gossiprpc.PacemanLastFortReply {
	msg := friendlyError(err)
	if msg == "" {
		log.Warn("paceman lastfort fetch failed", zap.String("account", account), zap.Error(err))
		msg = "stats lookup failed"
	}
	return gossiprpc.PacemanLastFortReply{Player: account, Error: msg}
}

func (p *api) personalBest(ctx context.Context, req gossiprpc.Request) any {
	log := monitor.TxnLogger(ctx, p.log)
	account := strings.TrimSpace(req.Account)
	window := normalizePBWindow(req.TimeWindow)
	if account == "" {
		return gossiprpc.PacemanPersonalBestReply{Window: window, Error: "missing account"}
	}

	resp, err := p.cachedUserPBs(ctx, account, req.IsPremium)
	if err != nil {
		return personalBestErrorReply(log, account, window, err)
	}
	return buildPersonalBestReply(account, window, resp)
}

func personalBestErrorReply(log *zap.Logger, account, window string, err error) gossiprpc.PacemanPersonalBestReply {
	msg := friendlyError(err)
	if msg == "" {
		log.Warn("paceman personal best fetch failed", zap.String("account", account), zap.Error(err))
		msg = "stats lookup failed"
	}
	return gossiprpc.PacemanPersonalBestReply{Player: account, Window: window, Error: msg}
}
