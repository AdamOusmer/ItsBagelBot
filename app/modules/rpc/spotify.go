// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/modules/repository"
	spotifyrpc "ItsBagelBot/internal/domain/rpc/spotify"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
)

// spotifyWiring bundles what wireSpotify needs beyond the subject prefixes
// (which it reads from the environment itself): the RPC connection, the token
// store, the shared queue group, and the New Relic app + logger.
type spotifyWiring struct {
	nc         *nats.Conn
	creds      *repository.SpotifyCreds
	queueGroup string
	app        *newrelic.Application
	log        *zap.Logger
}

// wireSpotify subscribes the Spotify refresh-token custody RPCs — the spotify
// twin of wireGovee. The dashboard verbs (set/clear/status) never echo the
// token; the internal decrypt verb is account-scoped to gossip, the one
// service that exchanges the token against accounts.spotify.com. It is a
// no-op when token custody is disabled (nil store).
func wireSpotify(w spotifyWiring) error {
	if w.creds == nil {
		return nil
	}
	dash := env.Get("NATS_MODULES_SPOTIFY_SUBJECT_PREFIX", "bagel.rpc.modules.spotify")
	internal := env.Get("NATS_INTERNAL_SPOTIFY_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.spotify.key")
	s := &spotifyRPC{creds: w.creds, log: w.log}

	if err := bus.QueueSubscribeJSON[spotifyrpc.RefreshTokenSetRequest, spotifyrpc.RefreshTokenMutateReply](
		w.nc, dash+".set", w.queueGroup, 3*time.Second, w.app, w.log, s.handleSet); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[spotifyrpc.RefreshTokenClearRequest, spotifyrpc.RefreshTokenMutateReply](
		w.nc, dash+".clear", w.queueGroup, 3*time.Second, w.app, w.log, s.handleClear); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[spotifyrpc.RefreshTokenStatusRequest, spotifyrpc.RefreshTokenStatusReply](
		w.nc, dash+".status", w.queueGroup, 3*time.Second, w.app, w.log, s.handleStatus); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[spotifyrpc.RefreshTokenGetRequest, spotifyrpc.RefreshTokenGetReply](
		w.nc, internal+".get", w.queueGroup, 3*time.Second, w.app, w.log, s.handleGet); err != nil {
		return err
	}
	if err := wireSpotifyApp(w, dash, s); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[spotifyrpc.RefreshTokenRotateRequest, spotifyrpc.RefreshTokenMutateReply](
		w.nc, internal+".rotate", w.queueGroup, 3*time.Second, w.app, w.log, s.handleRotate); err != nil {
		return err
	}
	w.log.Info("spotify token custody enabled", zap.String("dashboard_prefix", dash))
	return nil
}

// wireSpotifyApp subscribes the broadcaster-owned application verbs. They sit
// on the dashboard prefix beside set/clear/status because the console is what
// collects them; the secret only ever comes back out on the internal key.get
// subject gossip imports, never here.
func wireSpotifyApp(w spotifyWiring, dash string, s *spotifyRPC) error {
	if err := bus.QueueSubscribeJSON[spotifyrpc.AppSetRequest, spotifyrpc.RefreshTokenMutateReply](
		w.nc, dash+".app.set", w.queueGroup, 3*time.Second, w.app, w.log, s.handleAppSet); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[spotifyrpc.AppClearRequest, spotifyrpc.RefreshTokenMutateReply](
		w.nc, dash+".app.clear", w.queueGroup, 3*time.Second, w.app, w.log, s.handleAppClear); err != nil {
		return err
	}
	return bus.QueueSubscribeJSON[spotifyrpc.AppStatusRequest, spotifyrpc.AppStatusReply](
		w.nc, dash+".app.status", w.queueGroup, 3*time.Second, w.app, w.log, s.handleAppStatus)
}

type spotifyRPC struct {
	creds *repository.SpotifyCreds
	log   *zap.Logger
}

func (s *spotifyRPC) handleSet(ctx context.Context, req spotifyrpc.RefreshTokenSetRequest) spotifyrpc.RefreshTokenMutateReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return spotifyrpc.RefreshTokenMutateReply{Error: "user_id must be numeric"}
	}
	if err := s.creds.SetToken(ctx, id, req.RefreshToken); err != nil {
		// The error never carries the token; it is a validation or seal failure.
		return spotifyrpc.RefreshTokenMutateReply{Error: err.Error()}
	}
	return spotifyrpc.RefreshTokenMutateReply{}
}

func (s *spotifyRPC) handleClear(ctx context.Context, req spotifyrpc.RefreshTokenClearRequest) spotifyrpc.RefreshTokenMutateReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return spotifyrpc.RefreshTokenMutateReply{Error: "user_id must be numeric"}
	}
	if err := s.creds.ClearToken(ctx, id); err != nil {
		return spotifyrpc.RefreshTokenMutateReply{Error: err.Error()}
	}
	return spotifyrpc.RefreshTokenMutateReply{}
}

func (s *spotifyRPC) handleStatus(ctx context.Context, req spotifyrpc.RefreshTokenStatusRequest) spotifyrpc.RefreshTokenStatusReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return spotifyrpc.RefreshTokenStatusReply{Error: "user_id must be numeric"}
	}
	present, err := s.creds.HasToken(ctx, id)
	if err != nil {
		return spotifyrpc.RefreshTokenStatusReply{Error: err.Error()}
	}
	return spotifyrpc.RefreshTokenStatusReply{Present: present}
}

func (s *spotifyRPC) handleRotate(ctx context.Context, req spotifyrpc.RefreshTokenRotateRequest) spotifyrpc.RefreshTokenMutateReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return spotifyrpc.RefreshTokenMutateReply{Error: "user_id must be numeric"}
	}
	if err := s.creds.RotateToken(ctx, id, req.PrevToken, req.NewToken); err != nil {
		// Never carries a token: validation, seal or staleness.
		return spotifyrpc.RefreshTokenMutateReply{Error: err.Error()}
	}
	return spotifyrpc.RefreshTokenMutateReply{}
}

// handleGet answers gossip with the broadcaster's whole credential set. A
// broadcaster with no application of their own is an empty reply, not an
// error: gossip turns that into "set up Spotify in the console".
func (s *spotifyRPC) handleGet(ctx context.Context, req spotifyrpc.RefreshTokenGetRequest) spotifyrpc.RefreshTokenGetReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return spotifyrpc.RefreshTokenGetReply{Error: "user_id must be numeric"}
	}
	clientID, clientSecret, token, err := s.creds.Credentials(ctx, id)
	switch {
	case errors.Is(err, repository.ErrNoSpotifyApp), errors.Is(err, repository.ErrNoSpotifyToken):
		return spotifyrpc.RefreshTokenGetReply{}
	case err != nil:
		return spotifyrpc.RefreshTokenGetReply{Error: err.Error()}
	}
	return spotifyrpc.RefreshTokenGetReply{RefreshToken: token, ClientID: clientID, ClientSecret: clientSecret}
}

func (s *spotifyRPC) handleAppSet(ctx context.Context, req spotifyrpc.AppSetRequest) spotifyrpc.RefreshTokenMutateReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return spotifyrpc.RefreshTokenMutateReply{Error: "user_id must be numeric"}
	}
	if err := s.creds.SetApp(ctx, id, req.ClientID, req.ClientSecret); err != nil {
		// Never echoes the secret; this is a validation or seal failure.
		return spotifyrpc.RefreshTokenMutateReply{Error: err.Error()}
	}
	return spotifyrpc.RefreshTokenMutateReply{}
}

func (s *spotifyRPC) handleAppClear(ctx context.Context, req spotifyrpc.AppClearRequest) spotifyrpc.RefreshTokenMutateReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return spotifyrpc.RefreshTokenMutateReply{Error: "user_id must be numeric"}
	}
	if err := s.creds.ClearApp(ctx, id); err != nil {
		return spotifyrpc.RefreshTokenMutateReply{Error: err.Error()}
	}
	return spotifyrpc.RefreshTokenMutateReply{}
}

func (s *spotifyRPC) handleAppStatus(ctx context.Context, req spotifyrpc.AppStatusRequest) spotifyrpc.AppStatusReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return spotifyrpc.AppStatusReply{Error: "user_id must be numeric"}
	}
	present, clientID, err := s.creds.HasApp(ctx, id)
	if err != nil {
		return spotifyrpc.AppStatusReply{Error: err.Error()}
	}
	return spotifyrpc.AppStatusReply{Present: present, ClientID: clientID}
}
