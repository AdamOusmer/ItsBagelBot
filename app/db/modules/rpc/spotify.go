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

// wireSpotify subscribes the Spotify refresh-token custody RPCs: the spotify
// twin of wireGovee. The dashboard verbs (set/clear/status) never echo the
// token; the internal decrypt verb is account-scoped to gossip, the one
// service that exchanges the token against accounts.spotify.com. It is a
// no-op when token custody is disabled (nil store).
//
// The application verbs (app.set/clear/status) sit on the dashboard prefix
// beside set/clear/status because the console is what collects them; the
// client secret only ever comes back out on the internal key.get subject that
// gossip imports, never on those.
func wireSpotify(w bus.RPCWiring, creds *repository.SpotifyCreds) error {
	if creds == nil {
		return nil
	}
	dash := env.Get("NATS_MODULES_SPOTIFY_SUBJECT_PREFIX", "bagel.rpc.modules.spotify")
	internal := env.Get("NATS_INTERNAL_SPOTIFY_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.spotify.key")
	s := &spotifyRPC{creds: creds, log: w.Log}

	// One binding per verb: the eight verbs carry eight different request and
	// reply types and so cannot share one ServeVerbs table. The subject and
	// handler are all that varies; the wiring and its custody budget are bound
	// once, which is what stops one of them ending up on the wrong queue
	// group, and ServeForUser puts the fleet-wide user-id guard in front of
	// every one.
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

// No error a write surfaces ever carries a secret: the writes take their
// plaintexts as arguments and fail on validation, sealing or staleness.
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

// handleRotate persists a refresh token Spotify replaced mid-exchange.
func (s *spotifyRPC) handleRotate(ctx context.Context, req spotifyrpc.RefreshTokenRotateRequest, id uint64) (spotifyrpc.RefreshTokenMutateReply, error) {
	return spotifyrpc.RefreshTokenMutateReply{}, s.creds.RotateToken(ctx, id, req.PrevToken, req.NewToken)
}

// handleGet answers gossip with the broadcaster's whole credential set. A
// broadcaster with nothing set up is an empty reply, not an error: gossip
// turns that into "set up Spotify in the console".
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

// handleAppStatus reports the application by its client id alone: the store
// hands back nothing else on this path, so the secret cannot reach a
// dashboard-facing subject even by mistake.
func (s *spotifyRPC) handleAppStatus(ctx context.Context, _ spotifyrpc.AppStatusRequest, id uint64) (spotifyrpc.AppStatusReply, error) {
	clientID, err := s.creds.AppClientID(ctx, id)
	return spotifyrpc.AppStatusReply{Present: clientID != "", ClientID: clientID}, err
}
