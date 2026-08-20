// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/modules/repository"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"ItsBagelBot/pkg/bus"
)

// PersonalityWiring bundles what SubscribePersonality needs, mirroring the
// quotes wiring.
type PersonalityWiring struct {
	NC         *nats.Conn
	Repo       *repository.Personality
	Prefix     string // subject prefix, e.g. "bagel.rpc.modules.personality"
	QueueGroup string
	App        *newrelic.Application
	Log        *zap.Logger
}

// SubscribePersonality answers the personality verbs under w.Prefix: feed,
// which records one feeding on the fleet-wide counter and the feeding
// channel's row, and feed.board, the read-only leaderboard. They ride the
// MODULES_RPC account export like the quote verbs, but sesame's WORKER_RPC
// imports are scoped per subtree, so each verb needs its own import line in
// nats-auth.conf (a bare export is not enough for the request to cross
// accounts).
func SubscribePersonality(w PersonalityWiring) error {
	if err := subscribeFeedBump(w); err != nil {
		return err
	}
	return subscribeFeedBoard(w)
}

// subscribeFeedBump answers the write verb: one feeding, both counters.
func subscribeFeedBump(w PersonalityWiring) error {
	handler := func(ctx context.Context, req modulesrpc.FeedBumpRequest) modulesrpc.FeedBumpReply {
		totals, err := w.Repo.FeedBump(ctx, req.BroadcasterID, req.Name)
		if err != nil {
			return modulesrpc.FeedBumpReply{Error: err.Error()}
		}
		return modulesrpc.FeedBumpReply{Total: totals.Total, Channel: totals.Channel, Rank: totals.Rank}
	}
	subject := w.Prefix + ".feed"
	return bus.QueueSubscribeJSON[modulesrpc.FeedBumpRequest, modulesrpc.FeedBumpReply](w.NC, subject, w.QueueGroup, 2*time.Second, w.App, w.Log, handler)
}

// subscribeFeedBoard answers the read verb: the leaderboard, plus the asking
// channel's own standing when it named itself.
func subscribeFeedBoard(w PersonalityWiring) error {
	handler := func(ctx context.Context, req modulesrpc.FeedBoardRequest) modulesrpc.FeedBoardReply {
		reply, err := readFeedBoard(ctx, w.Repo, req)
		if err != nil {
			return modulesrpc.FeedBoardReply{Error: err.Error()}
		}
		return reply
	}
	subject := w.Prefix + ".feed.board"
	return bus.QueueSubscribeJSON[modulesrpc.FeedBoardRequest, modulesrpc.FeedBoardReply](w.NC, subject, w.QueueGroup, 2*time.Second, w.App, w.Log, handler)
}

// readFeedBoard collects the three reads a leaderboard answer needs, keeping
// the error handling out of the subscribe handler.
func readFeedBoard(ctx context.Context, repo *repository.Personality, req modulesrpc.FeedBoardRequest) (modulesrpc.FeedBoardReply, error) {
	board, err := readBoardEntries(ctx, repo, req.Limit)
	if err != nil {
		return modulesrpc.FeedBoardReply{}, err
	}
	ranked, err := repo.FeedRanked(ctx)
	if err != nil {
		return modulesrpc.FeedBoardReply{}, err
	}
	total, err := repo.FeedTotal(ctx)
	if err != nil {
		return modulesrpc.FeedBoardReply{}, err
	}
	reply := modulesrpc.FeedBoardReply{Entries: board, Total: total, Ranked: ranked}
	if req.BroadcasterID == 0 {
		return reply, nil
	}
	count, rank, err := repo.FeedChannel(ctx, req.BroadcasterID)
	if err != nil {
		return modulesrpc.FeedBoardReply{}, err
	}
	reply.Channel, reply.Rank = count, rank
	return reply, nil
}

// readBoardEntries skips the board query entirely for a negative limit: the
// standing-only command has no use for the podium.
func readBoardEntries(ctx context.Context, repo *repository.Personality, limit int) ([]modulesrpc.FeedBoardEntry, error) {
	if limit < 0 {
		return nil, nil
	}
	rows, err := repo.FeedBoard(ctx, limit)
	if err != nil {
		return nil, err
	}
	return toBoardEntries(rows), nil
}

func toBoardEntries(rows []repository.FeedBoardRow) []modulesrpc.FeedBoardEntry {
	entries := make([]modulesrpc.FeedBoardEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, modulesrpc.FeedBoardEntry{
			BroadcasterID: row.BroadcasterID, Name: row.Name, Count: row.Count,
		})
	}
	return entries
}
