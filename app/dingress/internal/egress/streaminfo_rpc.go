// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package egress

import (
	"context"
	"strings"
	"time"

	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

// streamInfoFallbackTimeout bounds liveInfo's one-shot outgress round trip.
// It must stay well under the go-live handler's own budget: liveInfo treats
// a timeout exactly like an empty projection (see live.go), so a slow
// outgress reply costs a stale/missing category, never a dropped post.
const streamInfoFallbackTimeout = 3 * time.Second

// streamInfoFallback is the interface liveInfo calls through; Worker holds
// one of these instead of *StreamInfoFallback directly so tests can inject
// a fake without a NATS connection.
type streamInfoFallback interface {
	Lookup(ctx context.Context, broadcasterID string) (projection.StreamInfo, bool)
}

// StreamInfoFallback is liveInfo's Helix-details escape hatch, reached over
// outgress's RPC rather than a Helix call made from this process -- see
// live.go's liveInfo for why. *StreamInfoFallback implements
// streamInfoFallback and is the production wiring; app/dingress/main.go
// builds it from outgress's own RPC prefix (config.OutgressRPCPrefix), not
// dingress's.
type StreamInfoFallback struct {
	request func(context.Context, outgressrpc.StreamInfoRequest) (outgressrpc.StreamInfoReply, error)
}

// NewStreamInfoFallback builds the outgress RPC client. prefix is outgress's
// bagel.rpc.outgress prefix.
func NewStreamInfoFallback(nc *nats.Conn, prefix string) *StreamInfoFallback {
	subject := strings.TrimSuffix(prefix, ".") + ".streaminfo.get"
	return &StreamInfoFallback{
		request: func(ctx context.Context, req outgressrpc.StreamInfoRequest) (outgressrpc.StreamInfoReply, error) {
			return bus.RequestJSONTimeout[outgressrpc.StreamInfoReply](ctx, nc, subject, req, streamInfoFallbackTimeout)
		},
	}
}

// Lookup asks outgress for the current Get Streams snapshot. ok is false on
// any RPC failure, an outgress-side error, or an offline stream -- liveInfo
// treats all three identically: keep whatever the projection already had.
func (f *StreamInfoFallback) Lookup(ctx context.Context, broadcasterID string) (projection.StreamInfo, bool) {
	reply, err := f.request(ctx, outgressrpc.StreamInfoRequest{BroadcasterID: broadcasterID})
	if err != nil || reply.Error != "" || !reply.Live {
		return projection.StreamInfo{}, false
	}
	return projection.StreamInfo{Title: reply.Title, GameName: reply.GameName, ViewerCount: reply.ViewerCount}, true
}

var _ streamInfoFallback = (*StreamInfoFallback)(nil)
