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

// SpotifyCredentials is one broadcaster's complete Spotify identity: the
// application they registered themselves plus the OAuth grant minted against
// it. The fleet holds no Spotify app of its own, so both halves are per
// broadcaster and both are needed for any call: a grant can only be refreshed
// by the application that issued it.
//
// An empty ClientID means the broadcaster has not registered an application;
// an empty RefreshToken means they registered one but never finished the
// connect flow. Both are ordinary states, not failures.
type SpotifyCredentials struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
}

// SpotifyKeyClient resolves a broadcaster's decrypted Spotify credentials over
// the modules service's internal RPC, gossip's twin of outgress's tokenstore
// and the spotify sibling of GoveeKeyClient. It returns the broadcaster's own
// application alongside the refresh token, since the fleet holds no Spotify
// app to fall back on. The plaintexts are used to mint one short-lived access
// token (cached separately, see the spotify provider's tokenCacheTTL) and are
// themselves never cached.
type SpotifyKeyClient struct {
	nc     *nats.Conn
	prefix string // e.g. "bagel.rpc.internal.spotify.key"
}

// NewSpotifyKeyClient builds the resolver against the modules internal
// refresh-token RPC.
func NewSpotifyKeyClient(nc *nats.Conn, prefix string) *SpotifyKeyClient {
	return &SpotifyKeyClient{nc: nc, prefix: prefix}
}

// Credentials returns the broadcaster's decrypted application and refresh
// token. A broadcaster with nothing on file comes back as a zero value and a
// nil error; a transport or service failure is returned as an error.
func (c *SpotifyKeyClient) Credentials(ctx context.Context, broadcasterID string) (SpotifyCredentials, error) {
	reply, err := bus.RequestJSONTimeout[spotifyrpc.RefreshTokenGetReply](
		ctx, c.nc, c.prefix+".get", spotifyrpc.RefreshTokenGetRequest{UserID: broadcasterID}, spotifyKeyTimeout)
	if err != nil {
		return SpotifyCredentials{}, fmt.Errorf("spotify key get rpc: %w", err)
	}
	if reply.Error != "" {
		return SpotifyCredentials{}, fmt.Errorf("spotify key get: %s", reply.Error)
	}
	return SpotifyCredentials{
		ClientID:     reply.ClientID,
		ClientSecret: reply.ClientSecret,
		RefreshToken: reply.RefreshToken,
	}, nil
}

// Rotate writes a replacement refresh token back to custody after Spotify
// rotated it on exchange. Compare-and-swap on the previous value: the modules
// store refuses the swap when someone else already replaced the token, and
// that staleness comes back as an error like any other failure: the caller
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
