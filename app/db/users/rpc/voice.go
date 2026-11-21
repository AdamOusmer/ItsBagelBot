// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/app/db/users/voice"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const voiceBudget = 3 * time.Second

func SubscribeVoice(w Wiring, checkSubject, recordSubject string) error {
	handler := voiceRPC{repo: w.Repo}
	rpc := w.Within(voiceBudget)
	if err := bus.ServeForUser[usersrpc.VoiceCheckRequest, voiceCheckResponse](rpc, checkSubject, handler.check); err != nil {
		return err
	}
	return bus.ServeForUser[usersrpc.VoiceRecordRequest, voiceResponse](rpc, recordSubject, handler.record)
}

type voiceRPC struct {
	repo *repository.Users
}

type voiceResponse struct {
	voice.Allowance
	domainrpc.Refusal
}

type voiceCheckResponse struct {
	voiceResponse
	Allowed bool `json:"allowed"`
}

func (v voiceRPC) check(ctx context.Context, _ usersrpc.VoiceCheckRequest, id uint64) (voiceCheckResponse, error) {
	allowance, err := v.repo.VoiceAllowance(ctx, id, time.Now())
	return voiceCheckResponse{
		voiceResponse: voiceReply(allowance, err),
		Allowed:       allowance.Enabled && allowance.RemainingTokens > 0,
	}, nil
}

func (v voiceRPC) record(ctx context.Context, req usersrpc.VoiceRecordRequest, id uint64) (voiceResponse, error) {
	allowance, err := v.repo.RecordVoiceUsage(ctx, id, repository.VoiceUsageRecord{
		RequestID: req.RequestID, AudioTokens: req.AudioTokens, DurationMS: req.DurationMS,
	}, time.Now())
	return voiceReply(allowance, err), nil
}

func voiceReply(allowance voice.Allowance, err error) voiceResponse {
	return voiceResponse{
		Allowance: allowance,
		Refusal: bus.Classify(err, entNotFound,
			domainrpc.Is(validate.ErrUserIDZero, domainrpc.CodeInvalid),
			domainrpc.Is(repository.ErrInvalidVoiceUsage, domainrpc.CodeInvalid)),
	}
}

func (d *dashboardRPC) handleVoiceGet(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.VoiceGetRequest](msg)
	if !ok {
		return
	}
	id, ok := dashboardVoiceOwner(msg, req)
	if !ok {
		return
	}
	allowance, err := d.repo.VoiceAllowance(ctx, id, time.Now())
	bus.Respond(msg, voiceReply(allowance, err))
}

func (d *dashboardRPC) handleVoiceSet(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.VoiceSetRequest](msg)
	if !ok {
		return
	}
	id, ok := dashboardVoiceOwner(msg, req.VoiceGetRequest)
	if !ok {
		return
	}
	err := d.repo.SetVoiceSettings(ctx, id, req.Enabled, req.Voice)
	if errors.Is(err, repository.ErrInvalidVoice) {
		respondErr(msg, "bad request")
		return
	}
	if err != nil {
		respondFail(msg, err)
		return
	}
	respondOK(msg)
}

func dashboardVoiceOwner(msg *nats.Msg, req usersrpc.VoiceGetRequest) (uint64, bool) {
	id, err := bus.NonZeroUserID(req.BroadcasterUserID)
	if err != nil {
		respondErr(msg, "bad request")
		return 0, false
	}
	actor, err := bus.NonZeroUserID(req.ActorID)
	if err != nil || actor != id {
		bus.Respond(msg, domainrpc.Refused(domainrpc.CodeForbidden, "forbidden"))
		return 0, false
	}
	return id, true
}
