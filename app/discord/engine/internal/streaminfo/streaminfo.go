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

// fallbackTimeout bounds one-shot Twitch-outgress round trip. It must stay
// well under the go-live handler's own budget: liveInfo treats a timeout
// exactly like an empty projection, so a slow outgress reply costs a
// stale/missing category, never a dropped post.
const fallbackTimeout = 3 * time.Second

// Fallback is the go-live module's Helix-details escape hatch, reached over
// TWITCH outgress's RPC (bagel.rpc.outgress, app/outgress) rather than a
// Helix call made from this process. Ported unchanged from
// app/dingress/internal/egress's StreamInfoFallback: engine has no Twitch
// client any more than dingress's ROLE=egress did, for the same reason (see
// modules/live.go's liveInfo).
type Fallback struct {
	request func(context.Context, outgressrpc.StreamInfoRequest) (outgressrpc.StreamInfoReply, error)
}

// New builds the Twitch-outgress RPC client. prefix is Twitch outgress's own
// bagel.rpc.outgress prefix, NOT app/discord/outgress's.
func New(nc *nats.Conn, prefix string) *Fallback {
	subject := strings.TrimSuffix(prefix, ".") + ".streaminfo.get"
	return &Fallback{
		request: func(ctx context.Context, req outgressrpc.StreamInfoRequest) (outgressrpc.StreamInfoReply, error) {
			return bus.RequestJSONTimeout[outgressrpc.StreamInfoReply](ctx, nc, subject, req, fallbackTimeout)
		},
	}
}

// Lookup asks Twitch outgress for the current Get Streams snapshot. ok is
// false on any RPC failure, an outgress-side error, or an offline stream --
// the caller treats all three identically: keep whatever the projection
// already had.
func (f *Fallback) Lookup(ctx context.Context, broadcasterID string) (projection.StreamInfo, bool) {
	reply, err := f.request(ctx, outgressrpc.StreamInfoRequest{BroadcasterID: broadcasterID})
	if err != nil || reply.Error != "" || !reply.Live {
		return projection.StreamInfo{}, false
	}
	return projection.StreamInfo{Title: reply.Title, GameName: reply.GameName, ViewerCount: reply.ViewerCount}, true
}
