// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"
	"fmt"
	"time"

	spotifyrpc "ItsBagelBot/internal/domain/rpc/spotify"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

// spotifyKeyTimeout bounds the internal refresh-token lookup. Same reasoning
// as goveeKeyTimeout: the modules service answers from its own database with
// no upstream hop, and the caller's handler budget carries the rest.
const spotifyKeyTimeout = 2 * time.Second

// SpotifyKeyClient resolves a broadcaster's decrypted Spotify OAuth refresh
// token over the modules service's internal RPC, gossip's twin of outgress's
// tokenstore and the spotify sibling of GoveeKeyClient. The plaintext token
// is used to mint one short-lived access token (cached separately, see the
// spotify provider's tokenCacheTTL) and is itself never cached.
type SpotifyKeyClient struct {
	nc     *nats.Conn
	prefix string // e.g. "bagel.rpc.internal.spotify.key"
}

// NewSpotifyKeyClient builds the resolver against the modules internal
// refresh-token RPC.
func NewSpotifyKeyClient(nc *nats.Conn, prefix string) *SpotifyKeyClient {
	return &SpotifyKeyClient{nc: nc, prefix: prefix}
}

// Key returns the broadcaster's decrypted Spotify refresh token, or "" (nil
// error) when none is on file. A transport or service failure is returned as
// an error.
func (c *SpotifyKeyClient) Key(ctx context.Context, broadcasterID string) (string, error) {
	reply, err := bus.RequestJSONTimeout[spotifyrpc.RefreshTokenGetReply](
		ctx, c.nc, c.prefix+".get", spotifyrpc.RefreshTokenGetRequest{UserID: broadcasterID}, spotifyKeyTimeout)
	if err != nil {
		return "", fmt.Errorf("spotify key get rpc: %w", err)
	}
	if reply.Error != "" {
		return "", fmt.Errorf("spotify key get: %s", reply.Error)
	}
	return reply.RefreshToken, nil
}

// Rotate writes a replacement refresh token back to custody after Spotify
// rotated it on exchange. Compare-and-swap on the previous value: the modules
// store refuses the swap when someone else already replaced the token, and
// that staleness comes back as an error like any other failure — the caller
// treats them all the same way (warn and keep serving on the token it has).
func (c *SpotifyKeyClient) Rotate(ctx context.Context, broadcasterID, prevToken, newToken string) error {
	reply, err := bus.RequestJSONTimeout[spotifyrpc.RefreshTokenMutateReply](
		ctx, c.nc, c.prefix+".rotate", spotifyrpc.RefreshTokenRotateRequest{
			UserID:    broadcasterID,
			PrevToken: prevToken,
			NewToken:  newToken,
		}, spotifyKeyTimeout)
	if err != nil {
		return fmt.Errorf("spotify key rotate rpc: %w", err)
	}
	if reply.Error != "" {
		return fmt.Errorf("spotify key rotate: %s", reply.Error)
	}
	return nil
}
