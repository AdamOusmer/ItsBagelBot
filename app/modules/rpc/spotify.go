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
// spotifyRPCTimeout bounds one custody handler. Every verb here answers from
// this service's own database with no upstream hop, so the budget is the same
// on all of them.
const spotifyRPCTimeout = 3 * time.Second

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

	// One binding per verb, each deferred so the subject/handler pair is all
	// that varies: the connection, queue group, timeout, New Relic app and
	// logger are identical on every subject here, and repeating them per verb
	// is how one of them ends up subscribed to the wrong group.
	for _, bind := range []func() error{
		func() error { return subscribeSpotify(w, dash+".set", s.handleSet) },
		func() error { return subscribeSpotify(w, dash+".clear", s.handleClear) },
		func() error { return subscribeSpotify(w, dash+".status", s.handleStatus) },
		func() error { return subscribeSpotify(w, dash+".app.set", s.handleAppSet) },
		func() error { return subscribeSpotify(w, dash+".app.clear", s.handleAppClear) },
		func() error { return subscribeSpotify(w, dash+".app.status", s.handleAppStatus) },
		func() error { return subscribeSpotify(w, internal+".get", s.handleGet) },
		func() error { return subscribeSpotify(w, internal+".rotate", s.handleRotate) },
	} {
		if err := bind(); err != nil {
			return err
		}
	}
	w.log.Info("spotify token custody enabled", zap.String("dashboard_prefix", dash))
	return nil
}

// subscribeSpotify binds one verb of the custody RPC. The request and reply
// types come from the handler, so a verb is a subject plus a method and
// nothing else.
//
// The application verbs (app.set/clear/status) sit on the dashboard prefix
// beside set/clear/status because the console is what collects them; the
// client secret only ever comes back out on the internal key.get subject that
// gossip imports, never on those.
func subscribeSpotify[Req any, Reply any](w spotifyWiring, subject string, h func(context.Context, Req) Reply) error {
	return bus.QueueSubscribeJSON[Req, Reply](w.nc, subject, w.queueGroup, spotifyRPCTimeout, w.app, w.log, h)
}

type spotifyRPC struct {
	creds *repository.SpotifyCreds
	log   *zap.Logger
}

// errNumericUserID is the one refusal every verb here shares: the wire carries
// the Twitch id as a string, and anything unparseable is a caller bug.
const errNumericUserID = "user_id must be numeric"

// userID parses the wire id. The bool, rather than an error, is what lets each
// handler spell its own reply envelope in one line — the four envelopes on
// these subjects differ, the parse does not.
func userID(raw string) (uint64, bool) {
	id, err := strconv.ParseUint(raw, 10, 64)
	return id, err == nil
}

// mutate runs one custody write and maps BOTH of its failure modes onto the
// shared ack envelope. Every write verb on these subjects — set, clear, and
// the two application verbs — is this shape and nothing else, so it is written
// once here rather than four times with four chances to drift.
//
// No error is ever echoed with a secret in it: the writes take their
// plaintexts as arguments and fail on validation or sealing.
func mutate(raw string, write func(uint64) error) spotifyrpc.RefreshTokenMutateReply {
	id, ok := userID(raw)
	if !ok {
		return spotifyrpc.RefreshTokenMutateReply{Error: errNumericUserID}
	}
	if err := write(id); err != nil {
		return spotifyrpc.RefreshTokenMutateReply{Error: err.Error()}
	}
	return spotifyrpc.RefreshTokenMutateReply{}
}

func (s *spotifyRPC) handleSet(ctx context.Context, req spotifyrpc.RefreshTokenSetRequest) spotifyrpc.RefreshTokenMutateReply {
	return mutate(req.UserID, func(id uint64) error { return s.creds.SetToken(ctx, id, req.RefreshToken) })
}

func (s *spotifyRPC) handleClear(ctx context.Context, req spotifyrpc.RefreshTokenClearRequest) spotifyrpc.RefreshTokenMutateReply {
	return mutate(req.UserID, func(id uint64) error { return s.creds.ClearToken(ctx, id) })
}

func (s *spotifyRPC) handleStatus(ctx context.Context, req spotifyrpc.RefreshTokenStatusRequest) spotifyrpc.RefreshTokenStatusReply {
	id, ok := userID(req.UserID)
	if !ok {
		return spotifyrpc.RefreshTokenStatusReply{Error: errNumericUserID}
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
// handleGet answers gossip with the broadcaster's whole credential set. A
// broadcaster with nothing set up is an empty reply, not an error: gossip
// turns that into "set up Spotify in the console".
func (s *spotifyRPC) handleGet(ctx context.Context, req spotifyrpc.RefreshTokenGetRequest) spotifyrpc.RefreshTokenGetReply {
	id, ok := userID(req.UserID)
	if !ok {
		return spotifyrpc.RefreshTokenGetReply{Error: errNumericUserID}
	}
	setup, err := s.creds.Credentials(ctx, id)
	switch {
	case errors.Is(err, repository.ErrNoSpotifyApp), errors.Is(err, repository.ErrNoSpotifyToken):
		return spotifyrpc.RefreshTokenGetReply{}
	case err != nil:
		return spotifyrpc.RefreshTokenGetReply{Error: err.Error()}
	}
	return spotifyrpc.RefreshTokenGetReply{
		RefreshToken: setup.RefreshToken,
		ClientID:     setup.App.ClientID,
		ClientSecret: setup.App.ClientSecret,
	}
}

func (s *spotifyRPC) handleAppSet(ctx context.Context, req spotifyrpc.AppSetRequest) spotifyrpc.RefreshTokenMutateReply {
	return mutate(req.UserID, func(id uint64) error {
		return s.creds.SetApp(ctx, id, repository.SpotifyApp{
			ClientID:     req.ClientID,
			ClientSecret: req.ClientSecret,
		})
	})
}

func (s *spotifyRPC) handleAppClear(ctx context.Context, req spotifyrpc.AppClearRequest) spotifyrpc.RefreshTokenMutateReply {
	return mutate(req.UserID, func(id uint64) error { return s.creds.ClearApp(ctx, id) })
}

// handleAppStatus reports the application by its client id alone — the store
// hands back nothing else on this path, so the secret cannot reach a
// dashboard-facing subject even by mistake.
func (s *spotifyRPC) handleAppStatus(ctx context.Context, req spotifyrpc.AppStatusRequest) spotifyrpc.AppStatusReply {
	id, ok := userID(req.UserID)
	if !ok {
		return spotifyrpc.AppStatusReply{Error: errNumericUserID}
	}
	clientID, err := s.creds.AppClientID(ctx, id)
	if err != nil {
		return spotifyrpc.AppStatusReply{Error: err.Error()}
	}
	return spotifyrpc.AppStatusReply{Present: clientID != "", ClientID: clientID}
}
