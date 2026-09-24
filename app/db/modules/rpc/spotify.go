// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/modules/repository"
	spotifyrpc "ItsBagelBot/internal/domain/rpc/spotify"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
)

// Dashboard verbs must never echo the token or client secret; only gossip's key.get returns them.
func wireSpotify(w bus.RPCWiring, creds *repository.SpotifyCreds) error {
	if creds == nil {
		return nil
	}
	dash := env.Get("NATS_MODULES_SPOTIFY_SUBJECT_PREFIX", "bagel.rpc.modules.spotify")
	internal := env.Get("NATS_INTERNAL_SPOTIFY_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.spotify.key")
	s := &spotifyRPC{creds: creds, log: w.Log}

	custody := w.Within(custodyBudget)
	if err := errors.Join(
		bus.ServeForUser[spotifyrpc.RefreshTokenSetRequest, spotifyrpc.RefreshTokenMutateReply](custody, dash+".set", s.handleSet),
		bus.ServeForUser[spotifyrpc.RefreshTokenClearRequest, spotifyrpc.RefreshTokenMutateReply](custody, dash+".clear", s.handleClear),
		bus.ServeForUser[spotifyrpc.RefreshTokenStatusRequest, spotifyrpc.RefreshTokenStatusReply](custody, dash+".status", s.handleStatus),
		bus.ServeForUser[spotifyrpc.AppSetRequest, spotifyrpc.RefreshTokenMutateReply](custody, dash+".app.set", s.handleAppSet),
		bus.ServeForUser[spotifyrpc.AppClearRequest, spotifyrpc.RefreshTokenMutateReply](custody, dash+".app.clear", s.handleAppClear),
		bus.ServeForUser[spotifyrpc.AppStatusRequest, spotifyrpc.AppStatusReply](custody, dash+".app.status", s.handleAppStatus),
		bus.ServeForUser[spotifyrpc.RefreshTokenGetRequest, spotifyrpc.RefreshTokenGetReply](custody, internal+".get", s.handleGet),
		bus.ServeForUser[spotifyrpc.RefreshTokenRotateRequest, spotifyrpc.RefreshTokenMutateReply](custody, internal+".rotate", s.handleRotate),
	); err != nil {
		return err
	}
	w.Log.Info("spotify token custody enabled", zap.String("dashboard_prefix", dash))
	return nil
}

type spotifyRPC struct {
	creds *repository.SpotifyCreds
	log   *zap.Logger
}

func (s *spotifyRPC) handleSet(ctx context.Context, req spotifyrpc.RefreshTokenSetRequest, id uint64) (spotifyrpc.RefreshTokenMutateReply, error) {
	return spotifyrpc.RefreshTokenMutateReply{}, s.creds.SetToken(ctx, id, repository.SpotifyGrant{
		RefreshToken: req.RefreshToken,
		Scopes:       req.Scopes,
	})
}

func (s *spotifyRPC) handleClear(ctx context.Context, _ spotifyrpc.RefreshTokenClearRequest, id uint64) (spotifyrpc.RefreshTokenMutateReply, error) {
	return spotifyrpc.RefreshTokenMutateReply{}, s.creds.ClearToken(ctx, id)
}

func (s *spotifyRPC) handleStatus(ctx context.Context, _ spotifyrpc.RefreshTokenStatusRequest, id uint64) (spotifyrpc.RefreshTokenStatusReply, error) {
	status, err := s.creds.TokenStatus(ctx, id)
	return spotifyrpc.RefreshTokenStatusReply{Present: status.Present, Scopes: status.Scopes}, err
}

func (s *spotifyRPC) handleRotate(ctx context.Context, req spotifyrpc.RefreshTokenRotateRequest, id uint64) (spotifyrpc.RefreshTokenMutateReply, error) {
	return spotifyrpc.RefreshTokenMutateReply{}, s.creds.RotateToken(ctx, id, req.PrevToken, req.NewToken)
}

func (s *spotifyRPC) handleGet(ctx context.Context, _ spotifyrpc.RefreshTokenGetRequest, id uint64) (spotifyrpc.RefreshTokenGetReply, error) {
	setup, err := s.creds.Credentials(ctx, id)
	if errors.Is(err, repository.ErrNoSpotifyApp) || errors.Is(err, repository.ErrNoSpotifyToken) {
		return spotifyrpc.RefreshTokenGetReply{}, nil
	}
	return spotifyrpc.RefreshTokenGetReply{
		RefreshToken: setup.RefreshToken,
		ClientID:     setup.App.ClientID,
		ClientSecret: setup.App.ClientSecret,
	}, err
}

func (s *spotifyRPC) handleAppSet(ctx context.Context, req spotifyrpc.AppSetRequest, id uint64) (spotifyrpc.RefreshTokenMutateReply, error) {
	return spotifyrpc.RefreshTokenMutateReply{}, s.creds.SetApp(ctx, id, repository.SpotifyApp{
		ClientID:     req.ClientID,
		ClientSecret: req.ClientSecret,
	})
}

func (s *spotifyRPC) handleAppClear(ctx context.Context, _ spotifyrpc.AppClearRequest, id uint64) (spotifyrpc.RefreshTokenMutateReply, error) {
	return spotifyrpc.RefreshTokenMutateReply{}, s.creds.ClearApp(ctx, id)
}

func (s *spotifyRPC) handleAppStatus(ctx context.Context, _ spotifyrpc.AppStatusRequest, id uint64) (spotifyrpc.AppStatusReply, error) {
	clientID, err := s.creds.AppClientID(ctx, id)
	return spotifyrpc.AppStatusReply{Present: clientID != "", ClientID: clientID}, err
}
