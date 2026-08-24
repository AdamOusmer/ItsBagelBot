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
	w.log.Info("spotify token custody enabled", zap.String("dashboard_prefix", dash))
	return nil
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

func (s *spotifyRPC) handleGet(ctx context.Context, req spotifyrpc.RefreshTokenGetRequest) spotifyrpc.RefreshTokenGetReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return spotifyrpc.RefreshTokenGetReply{Error: "user_id must be numeric"}
	}
	token, err := s.creds.Token(ctx, id)
	switch {
	case errors.Is(err, repository.ErrNoSpotifyToken):
		return spotifyrpc.RefreshTokenGetReply{}
	case err != nil:
		return spotifyrpc.RefreshTokenGetReply{Error: err.Error()}
	}
	return spotifyrpc.RefreshTokenGetReply{RefreshToken: token}
}
