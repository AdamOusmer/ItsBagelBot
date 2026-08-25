// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/app/outgress/internal/youtube"
	"ItsBagelBot/pkg/bus"
)

// ytTokenRPCTimeout bounds one token-lease RPC. The users service answers from
// its own database, so this matches the 2s every other internal token RPC in
// this service uses (token_lease.go's release budget, tokenstore's rpcTimeout):
// a miss costs one paced nack on a lane whose whole MaxAge is 5s, so waiting
// longer than ~2s would burn most of a message's useful life on one attempt.
const ytTokenRPCTimeout = 2 * time.Second

// ytTokenReply is ONE HALF of a byte-level contract shared with the Elixir
// consumer app/yt-ingress/lib/yt_ingress/token_source.ex: it requests
// {"channel_id": "..."} and reads {"channel_id", "access_token", "expires_at"},
// where expires_at is unix SECONDS (the consumer caches until
// expires_at - margin). Field names and units are fixed across services; do
// not rename or re-unit them unilaterally. channel_id is echoed but unused
// here — LeasedTokens keys its cache by the requested id already.
type ytTokenReply struct {
	ChannelID   string `json:"channel_id"`
	AccessToken string `json:"access_token"`
	ExpiresAt   int64  `json:"expires_at"` // unix seconds
}

// newYouTubeTokens picks the credential mode for Data API writes:
//
//   - YOUTUBE_BOT_REFRESH_TOKEN set: development. One bot-identity access
//     token minted straight from Google serves every channel; no per-channel
//     leasing exists to stand up locally.
//   - Otherwise: production. Short-lived per-channel tokens are leased over
//     NATS RPC from the users service (which owns the refresh tokens per the
//     data-and-state ownership rules), cached by channel until expiry minus
//     margin inside LeasedTokens.
func (d *deps) newYouTubeTokens() youtube.TokenSource {
	if d.cfg.YouTubeBotRefreshToken != "" {
		d.log.Info("youtube writes use the dev bot refresh token")
		return youtube.NewBotTokenSource(d.cfg.YouTubeClientID, d.cfg.YouTubeClientSecret, d.cfg.YouTubeBotRefreshToken)
	}
	return youtube.NewLeasedSource(youtube.NewLeasedTokens(d.youTubeLeaseToken))
}

// youTubeLeaseToken performs one bagel.rpc.youtube.token.get round trip for
// channelID (see ytTokenReply for the frozen contract). A missing access token
// in an otherwise well-formed reply is surfaced as an error rather than a zero
// value, so LeasedTokens never caches "no credential" as if it were one.
func (d *deps) youTubeLeaseToken(ctx context.Context, channelID string) (string, time.Time, error) {
	reply, err := bus.RequestJSONTimeout[ytTokenReply](ctx, d.nc, d.cfg.YouTubeTokenSubject,
		map[string]string{"channel_id": channelID}, ytTokenRPCTimeout)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("youtube token lease rpc: %w", err)
	}
	if reply.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("youtube token lease rpc: empty access token for %s", channelID)
	}
	return reply.AccessToken, time.Unix(reply.ExpiresAt, 0), nil
}

// startYTLifecycleLane binds a durable queue-grouped consumer for yt-ingress's
// stream.online / stream.offline events (YOUTUBE_INGRESS, which outgress
// reconciles because nothing else creates it today) under outgress's own
// group, feeding the shared live-chat directory the YouTube handlers resolve
// their target chat from. There is deliberately NO discovery fallback behind
// this feed (see youtube.ChatDirectory): a missed event drops sends loudly
// until the next lifecycle transition repopulates, instead of silently paying
// search.list's 100 units per send. HandleLifecycleEvent always acks — a
// malformed event must never poison or replay the lane.
func (d *deps) startYTLifecycleLane(ctx context.Context, directory *youtube.ChatDirectory) func() {
	lifecycleSub, err := bus.NewSubscriber(d.cfg.NATSURL, serviceName, d.log)
	fatalIf(d.log, err, "failed to connect youtube lifecycle subscriber")

	fatalIf(d.log, bus.Consume(ctx, d.nrApp, lifecycleSub, d.cfg.YouTubeStreamSubject,
		directory.HandleLifecycleEvent, d.log.Named("yt-lifecycle")),
		"failed to consume youtube lifecycle lane")

	return func() { _ = lifecycleSub.Close() }
}
