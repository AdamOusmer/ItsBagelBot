// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package streaminfo

import (
	"context"
	"strings"
	"time"

	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const fallbackTimeout = 3 * time.Second

type Fallback struct {
	request func(context.Context, outgressrpc.StreamInfoRequest) (outgressrpc.StreamInfoReply, error)
}

func New(nc *nats.Conn, prefix string) *Fallback {
	subject := strings.TrimSuffix(prefix, ".") + ".streaminfo.get"
	return &Fallback{
		request: func(ctx context.Context, req outgressrpc.StreamInfoRequest) (outgressrpc.StreamInfoReply, error) {
			return bus.RequestJSONTimeout[outgressrpc.StreamInfoReply](ctx, nc, subject, req, fallbackTimeout)
		},
	}
}

func lookupFailed(err error, reply outgressrpc.StreamInfoReply) bool {
	return err != nil || reply.Error != "" || !reply.Live
}

func (f *Fallback) Lookup(ctx context.Context, broadcasterID string) (projection.StreamInfo, bool) {
	reply, err := f.request(ctx, outgressrpc.StreamInfoRequest{BroadcasterID: broadcasterID})
	if lookupFailed(err, reply) {
		return projection.StreamInfo{}, false
	}
	return projection.StreamInfo{Title: reply.Title, GameName: reply.GameName, ViewerCount: reply.ViewerCount}, true
}
