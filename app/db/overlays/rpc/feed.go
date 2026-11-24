// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"strconv"

	"ItsBagelBot/app/db/overlays/livefeed"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/bus"
)

func SubscribeFeed(w bus.RPCWiring, feed *livefeed.Service) error {
	read := w.Within(readBudget)
	return errors.Join(
		bus.ServeForUser(read, overlaysrpc.FeedSnapshotSubject, feedSnapshot(feed)),
		bus.ServeForUser(read, overlaysrpc.FeedControlSubject, feedControl(feed)),
	)
}

func feedRefusal(err error) domainrpc.Refusal {
	return bus.Classify(err, overlaysrpc.FeedRules()...)
}

func feedSnapshot(feed *livefeed.Service) func(context.Context, overlaysrpc.FeedSnapshotRequest, uint64) (overlaysrpc.FeedSnapshotReply, error) {
	return func(ctx context.Context, req overlaysrpc.FeedSnapshotRequest, id uint64) (overlaysrpc.FeedSnapshotReply, error) {
		reply, err := feed.Snapshot(ctx, strconv.FormatUint(id, 10), req.Events)
		reply.Refusal = feedRefusal(err)
		return reply, nil
	}
}

func feedControl(feed *livefeed.Service) func(context.Context, overlaysrpc.FeedControlRequest, uint64) (overlaysrpc.FeedControlReply, error) {
	return func(ctx context.Context, req overlaysrpc.FeedControlRequest, _ uint64) (overlaysrpc.FeedControlReply, error) {
		reply, err := feed.Control(ctx, req)
		reply.Refusal = feedRefusal(err)
		return reply, nil
	}
}
