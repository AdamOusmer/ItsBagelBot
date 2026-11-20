// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const giveawayWinnersRPCTimeout = 3 * time.Second

type GiveawayWinnersStore interface {
	MarkDelivered(context.Context, uint64, modulesrpc.GiveawayDelivery) (bool, error)
	Record(ctx context.Context, broadcasterID uint64, rows []modulesrpc.GiveawayWinner) error
	WonWithin(ctx context.Context, broadcasterID uint64, viewers []uint64, since time.Time) ([]uint64, error)
	Recent(ctx context.Context, broadcasterID uint64, q modulesrpc.GiveawayRecentQuery) ([]modulesrpc.GiveawayWinner, error)
}

type GiveawayWinnersRPC struct {
	nc     *nats.Conn
	prefix string
}

func NewGiveawayWinnersRPC(nc *nats.Conn, modulesPrefix string) *GiveawayWinnersRPC {
	return &GiveawayWinnersRPC{nc: nc, prefix: modulesPrefix + modulesrpc.GiveawaySuffix}
}

func (c *GiveawayWinnersRPC) call(ctx context.Context, verb string, req modulesrpc.GiveawayRequest) (modulesrpc.GiveawayReply, error) {
	return bus.RequestJSONTimeout[modulesrpc.GiveawayReply](ctx, c.nc, c.prefix+"."+verb, req, giveawayWinnersRPCTimeout)
}

func (c *GiveawayWinnersRPC) Record(ctx context.Context, broadcasterID uint64, rows []modulesrpc.GiveawayWinner) error {
	_, err := c.call(ctx, modulesrpc.VerbGiveawayRecord, modulesrpc.GiveawayRequest{UserID: fmtID(broadcasterID), Winners: rows})
	return err
}

func (c *GiveawayWinnersRPC) Recent(ctx context.Context, broadcasterID uint64, q modulesrpc.GiveawayRecentQuery) ([]modulesrpc.GiveawayWinner, error) {
	req := modulesrpc.GiveawayRequest{UserID: fmtID(broadcasterID), RoundID: q.RoundID, Limit: q.Limit, Offset: q.Offset}
	reply, err := c.call(ctx, modulesrpc.VerbGiveawayRecent, req)
	return reply.Winners, err
}

func (c *GiveawayWinnersRPC) WonWithin(ctx context.Context, broadcasterID uint64, viewers []uint64, since time.Time) ([]uint64, error) {
	viewerIDs := make([]string, len(viewers))
	for i, id := range viewers {
		viewerIDs[i] = fmtID(id)
	}
	req := modulesrpc.GiveawayRequest{UserID: fmtID(broadcasterID), ViewerIDs: viewerIDs, SinceMs: since.UnixMilli()}
	reply, err := c.call(ctx, modulesrpc.VerbGiveawayWonWithin, req)
	if err != nil {
		return nil, err
	}
	return parseGiveawayIDs(reply.ViewerIDs)
}

func parseGiveawayIDs(viewers []string) ([]uint64, error) {
	ids := make([]uint64, len(viewers))
	for i, viewer := range viewers {
		id, err := bus.NonZeroUserID(viewer)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

func (c *GiveawayWinnersRPC) MarkDelivered(ctx context.Context, broadcasterID uint64, delivery modulesrpc.GiveawayDelivery) (bool, error) {
	req := modulesrpc.GiveawayRequest{UserID: fmtID(broadcasterID), RoundID: delivery.RoundID,
		Slot: delivery.Slot, Delivered: delivery.Delivered}
	reply, err := c.call(ctx, modulesrpc.VerbGiveawayDeliver, req)
	return reply.Found, err
}
