// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"time"

	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const personalityRPCTimeout = 2 * time.Second

type PersonalityRPC struct {
	nc           *nats.Conn
	subject      string
	boardSubject string
}

func NewPersonalityRPC(nc *nats.Conn, modulesPrefix string) *PersonalityRPC {
	subject := modulesPrefix + ".personality.feed"
	return &PersonalityRPC{nc: nc, subject: subject, boardSubject: subject + ".board"}
}

func (c *PersonalityRPC) FeedBump(ctx context.Context, broadcasterID uint64, name string) (FeedTotals, error) {
	request := modulesrpc.FeedBumpRequest{BroadcasterID: broadcasterID, Name: name}
	reply, err := bus.RequestJSONTimeout[modulesrpc.FeedBumpReply](ctx, c.nc, c.subject, request, personalityRPCTimeout)
	if err != nil {
		return FeedTotals{}, err
	}
	if reply.Error != "" {
		return FeedTotals{}, errors.New(reply.Error)
	}
	return FeedTotals{Total: reply.Total, Channel: reply.Channel, Rank: reply.Rank}, nil
}

func (c *PersonalityRPC) FeedBoard(ctx context.Context, broadcasterID uint64, limit int) (FeedBoard, error) {
	request := modulesrpc.FeedBoardRequest{Limit: limit, BroadcasterID: broadcasterID}
	reply, err := bus.RequestJSONTimeout[modulesrpc.FeedBoardReply](ctx, c.nc, c.boardSubject, request, personalityRPCTimeout)
	if err != nil {
		return FeedBoard{}, err
	}
	if reply.Error != "" {
		return FeedBoard{}, errors.New(reply.Error)
	}
	return FeedBoard{
		Entries: toEngineEntries(reply.Entries),
		Ranked:  reply.Ranked,
		Channel: reply.Channel,
		Rank:    reply.Rank,
	}, nil
}

func toEngineEntries(entries []modulesrpc.FeedBoardEntry) []FeedBoardEntry {
	if len(entries) == 0 {
		return nil
	}
	board := make([]FeedBoardEntry, 0, len(entries))
	for _, entry := range entries {
		board = append(board, FeedBoardEntry{
			BroadcasterID: entry.BroadcasterID, Name: entry.Name, Count: entry.Count,
		})
	}
	return board
}
