// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/db/users/repository"
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
	if err := bus.ServeForUser[usersrpc.VoiceCheckRequest, usersrpc.VoiceCheckReply](rpc, checkSubject, handler.check); err != nil {
		return err
	}
	return bus.ServeForUser[usersrpc.VoiceRecordRequest, usersrpc.VoiceReply](rpc, recordSubject, handler.record)
}

type voiceRPC struct {
	repo *repository.Users
}

func (v voiceRPC) check(ctx context.Context, _ usersrpc.VoiceCheckRequest, id uint64) (usersrpc.VoiceCheckReply, error) {
	allowance, err := v.repo.VoiceAllowance(ctx, id, time.Now())
	return usersrpc.VoiceCheckReply{
		VoiceReply: voiceReply(allowance, err),
		Allowed:    allowance.RemainingTokens > 0,
	}, nil
}

func (v voiceRPC) record(ctx context.Context, req usersrpc.VoiceRecordRequest, id uint64) (usersrpc.VoiceReply, error) {
	allowance, err := v.repo.RecordVoiceUsage(ctx, id, repository.VoiceUsageRecord{
		RequestID: req.RequestID, AudioTokens: req.AudioTokens, DurationMS: req.DurationMS,
	}, time.Now())
	return voiceReply(allowance, err), nil
}

func voiceReply(allowance usersrpc.VoiceAllowance, err error) usersrpc.VoiceReply {
	return usersrpc.VoiceReply{
		VoiceAllowance: allowance,
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

func (d *dashboardRPC) handleVoiceSafety(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.VoiceGetRequest](msg)
	if !ok {
		return
	}
	id, ok := dashboardVoiceOwner(msg, req)
	if !ok {
		return
	}
	channelID := strconv.FormatUint(id, 10)
	paused, err := d.safety.Rules.ChannelPausedUntil(ctx, d.safety.Store, channelID)
	if err != nil {
		respondFail(msg, err)
		return
	}
	blocks, err := d.safety.Rules.ChannelBlocks(ctx, d.safety.Store, channelID)
	if err != nil {
		respondFail(msg, err)
		return
	}
	reply := usersrpc.VoiceSafetyReply{Blocks: make([]usersrpc.VoiceStrikeBlock, 0, len(blocks))}
	if !paused.IsZero() {
		reply.PausedUntil = paused.UnixMilli()
	}
	for _, block := range blocks {
		reply.Blocks = append(reply.Blocks, usersrpc.VoiceStrikeBlock{
			ViewerID: block.ViewerID, Scope: string(block.Scope), Until: block.Until.UnixMilli(),
		})
	}
	bus.Respond(msg, reply)
}

func (d *dashboardRPC) handleVoiceUnblock(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.VoiceUnblockRequest](msg)
	if !ok {
		return
	}
	id, ok := dashboardVoiceOwner(msg, req.VoiceGetRequest)
	if !ok {
		return
	}
	if req.ViewerID == "" {
		respondErr(msg, "viewer_id required")
		return
	}
	if err := d.safety.Rules.ClearViewer(ctx, d.safety.Store, strconv.FormatUint(id, 10), req.ViewerID); err != nil {
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
