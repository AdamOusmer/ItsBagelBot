// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"

	spotifyrpc "ItsBagelBot/internal/domain/rpc/spotify"

	"github.com/nats-io/nats.go"
)

type SpotifyCredentials struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
}

type SpotifyKeyClient struct {
	get    *KeyClient[spotifyrpc.RefreshTokenGetRequest, spotifyrpc.RefreshTokenGetReply]
	rotate *KeyClient[spotifyrpc.RefreshTokenRotateRequest, spotifyrpc.RefreshTokenMutateReply]
}

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

func (c *SpotifyKeyClient) Rotate(ctx context.Context, broadcasterID, prevToken, newToken string) error {
	_, err := c.rotate.Call(ctx, spotifyrpc.RefreshTokenRotateRequest{
		UserID:    broadcasterID,
		PrevToken: prevToken,
		NewToken:  newToken,
	})
	return err
}
