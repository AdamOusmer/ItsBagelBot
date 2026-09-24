// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/db/modules/repository"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"ItsBagelBot/pkg/bus"
)

// Each verb needs its own sesame import line in nats-auth.conf.
func SubscribePersonality(w bus.RPCWiring, repo *repository.Personality, prefix string) error {
	if err := bus.Serve(w, prefix+".feed", feedBump(repo)); err != nil {
		return err
	}
	return bus.Serve(w, prefix+".feed.board", feedBoard(repo))
}

func feedBump(repo *repository.Personality) func(context.Context, modulesrpc.FeedBumpRequest) modulesrpc.FeedBumpReply {
	return func(ctx context.Context, req modulesrpc.FeedBumpRequest) modulesrpc.FeedBumpReply {
		totals, err := repo.FeedBump(ctx, req.BroadcasterID, req.Name, req.EventID)
		if err != nil {
			return modulesrpc.FeedBumpReply{Refusal: bus.Classify(err)}
		}
		return modulesrpc.FeedBumpReply{Total: totals.Total, Channel: totals.Channel, Rank: totals.Rank}
	}
}

func feedBoard(repo *repository.Personality) func(context.Context, modulesrpc.FeedBoardRequest) modulesrpc.FeedBoardReply {
	return func(ctx context.Context, req modulesrpc.FeedBoardRequest) modulesrpc.FeedBoardReply {
		reply, err := readFeedBoard(ctx, repo, req)
		if err != nil {
			return modulesrpc.FeedBoardReply{Refusal: bus.Classify(err)}
		}
		return reply
	}
}

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
