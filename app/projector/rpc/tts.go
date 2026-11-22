// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"strconv"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/internal/domain/event/data"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	projectionrpc "ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
)

type ttsRPC struct {
	nc           *nats.Conn
	store        *projection.Store
	usersSubject string
}

func SubscribeTts(w bus.RPCWiring, store *projection.Store, usersSubject string) error {
	s := ttsRPC{nc: w.NC, store: store, usersSubject: usersSubject}
	w = w.Within(poweredReplyTimeout)
	return errors.Join(
		bus.Serve(w, overlaysrpc.TtsResolveSubject, s.resolve),
		bus.Serve(w, overlaysrpc.TtsCheckSubject, s.check),
		bus.ServeForUser(w, overlaysrpc.TtsSettingsSubject, s.settings),
	)
}

func (s ttsRPC) resolve(ctx context.Context, req overlaysrpc.TtsResolveRequest) overlaysrpc.TtsResolveReply {
	if !req.TokenHash.Valid() {
		return ttsNotFound()
	}
	o, found, err := s.byHash(ctx, req)
	if err != nil {
		return overlaysrpc.TtsResolveReply{Refusal: domainrpc.Fail(err)}
	}
	if !found {
		return ttsNotFound()
	}
	u, err := s.user(ctx, o.UserID)
	if err != nil {
		return overlaysrpc.TtsResolveReply{Refusal: domainrpc.Fail(err)}
	}
	if !eligibleTtsUser(u, o) {
		return ttsNotFound()
	}
	return overlaysrpc.TtsResolveReply{UserID: o.UserID, Generation: o.Generation}
}

func ttsNotFound() overlaysrpc.TtsResolveReply {
	return overlaysrpc.TtsResolveReply{Refusal: domainrpc.Refused(domainrpc.CodeNotFound, "overlay token not found")}
}

func (s ttsRPC) check(ctx context.Context, req overlaysrpc.TtsCheckRequest) overlaysrpc.TtsCheckReply {
	u, err := s.user(ctx, req.UserID)
	if err != nil || !hasVoicePlan(u) {
		return overlaysrpc.TtsCheckReply{}
	}
	o, found, err := s.byUser(ctx, req.UserID)
	valid := err == nil && found && o.Configured()
	return overlaysrpc.TtsCheckReply{Valid: valid && o.Generation == req.Generation && eligibleTtsUser(u, o)}
}

func (s ttsRPC) settings(ctx context.Context, _ overlaysrpc.TtsUserRequest, id uint64) (overlaysrpc.TtsSettingsReply, error) {
	u, err := s.user(ctx, id)
	if err != nil || !hasVoicePlan(u) {
		return overlaysrpc.TtsSettingsReply{}, err
	}
	o, found, err := s.byUser(ctx, id)
	if err != nil || !found {
		return overlaysrpc.TtsSettingsReply{}, err
	}
	return overlaysrpc.TtsSettingsReply{Settings: o.Settings, Configured: o.Configured() && eligibleTtsUser(u, o)}, nil
}

func (s ttsRPC) byUser(ctx context.Context, id uint64) (overlaysrpc.TtsOverlay, bool, error) {
	req := overlaysrpc.TtsUserRequest{UserID: strconv.FormatUint(id, 10)}
	return s.load(ctx, ttsLookup{
		subject: overlaysrpc.OwnerTtsByUserSubject, request: req,
		read: func(ctx context.Context) (overlaysrpc.TtsOverlay, bool, error) { return s.store.TtsOverlay(ctx, id) },
	})
}

func (s ttsRPC) byHash(ctx context.Context, req overlaysrpc.TtsResolveRequest) (overlaysrpc.TtsOverlay, bool, error) {
	return s.load(ctx, ttsLookup{
		subject: overlaysrpc.OwnerTtsByHashSubject, request: req,
		read: func(ctx context.Context) (overlaysrpc.TtsOverlay, bool, error) {
			return s.store.TtsOverlayByHash(ctx, req.TokenHash)
		},
	})
}

type ttsLookup struct {
	subject string
	request any
	read    func(context.Context) (overlaysrpc.TtsOverlay, bool, error)
}

func (s ttsRPC) load(ctx context.Context, lookup ttsLookup) (overlaysrpc.TtsOverlay, bool, error) {
	o, found, err := lookup.read(ctx)
	if found || err != nil {
		return o, found, err
	}
	reply, err := bus.RequestJSONTimeout[overlaysrpc.OwnerTtsReply](ctx, s.nc, lookup.subject, lookup.request, poweredOwnerTimeout)
	if bus.NotFoundReply(err) {
		return o, false, nil
	}
	if err != nil || reply.Overlay == nil {
		return o, false, err
	}
	if _, err := s.store.FoldTtsOverlay(ctx, data.TtsOverlayChangedDTO{Overlay: *reply.Overlay}); err != nil {
		return o, false, err
	}
	return lookup.read(ctx)
}

func (s ttsRPC) user(ctx context.Context, id uint64) (projection.User, error) {
	u, found, err := s.store.GetUserRuntime(ctx, id)
	if err != nil {
		return u, err
	}
	if found && u.VoicePlan != "" {
		return u, nil
	}
	reply, err := bus.RequestJSONTimeout[projectionrpc.UserReply](ctx, s.nc, s.usersSubject,
		projectionrpc.Request{UserID: strconv.FormatUint(id, 10)}, poweredOwnerTimeout)
	if bus.NotFoundReply(err) {
		return projection.User{}, nil
	}
	if err != nil {
		return projection.User{}, err
	}
	if err := s.store.SetUser(ctx, id, projection.UserFromReply(reply)); err != nil {
		return projection.User{}, err
	}
	u, _, err = s.store.GetUserRuntime(ctx, id)
	return u, err
}

func eligibleTtsUser(u projection.User, o overlaysrpc.TtsOverlay) bool {
	return hasVoicePlan(u) && o.CreatedAt >= u.AccountCreatedAt
}

func hasVoicePlan(u projection.User) bool {
	return u.Premium() && !u.Banned && voiceplan.MonthlySeconds(voiceplan.Plan(u.VoicePlan)) > 0
}
