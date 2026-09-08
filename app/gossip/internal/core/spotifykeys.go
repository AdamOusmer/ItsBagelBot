// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"

	spotifyrpc "ItsBagelBot/internal/domain/rpc/spotify"

	"github.com/nats-io/nats.go"
)

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
	get    *KeyClient[spotifyrpc.RefreshTokenGetRequest, spotifyrpc.RefreshTokenGetReply]
	rotate *KeyClient[spotifyrpc.RefreshTokenRotateRequest, spotifyrpc.RefreshTokenMutateReply]
}

// NewSpotifyKeyClient builds the resolver against the modules internal
// refresh-token RPC. prefix is e.g. "bagel.rpc.internal.spotify.key".
func NewSpotifyKeyClient(nc *nats.Conn, prefix string) *SpotifyKeyClient {
	return &SpotifyKeyClient{
		get: newKeyClient[spotifyrpc.RefreshTokenGetRequest](keyClientConfig[spotifyrpc.RefreshTokenGetReply]{
			NC:       nc,
			Subject:  prefix + ".get",
			Label:    "spotify key get",
			ReplyErr: func(r spotifyrpc.RefreshTokenGetReply) string { return r.Error },
		}),
		rotate: newKeyClient[spotifyrpc.RefreshTokenRotateRequest](keyClientConfig[spotifyrpc.RefreshTokenMutateReply]{
			NC:       nc,
			Subject:  prefix + ".rotate",
			Label:    "spotify key rotate",
			ReplyErr: func(r spotifyrpc.RefreshTokenMutateReply) string { return r.Error },
		}),
	}
}

// Credentials returns the broadcaster's decrypted application and refresh
// token. A broadcaster with nothing on file comes back as a zero value and a
// nil error; a transport or service failure is returned as an error.
func (c *SpotifyKeyClient) Credentials(ctx context.Context, broadcasterID string) (SpotifyCredentials, error) {
	reply, err := c.get.Call(ctx, spotifyrpc.RefreshTokenGetRequest{UserID: broadcasterID})
	if err != nil {
		return SpotifyCredentials{}, err
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
	_, err := c.rotate.Call(ctx, spotifyrpc.RefreshTokenRotateRequest{
		UserID:    broadcasterID,
		PrevToken: prevToken,
		NewToken:  newToken,
	})
	return err
}
