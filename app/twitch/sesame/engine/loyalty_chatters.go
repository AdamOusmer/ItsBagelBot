// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nuid"
)

func (s *ValkeyLoyaltyClock) fetchPage(ctx context.Context, id uint64, state loyaltySchedule, checkLive bool) (manage.ChattersReply, error) {
	ctx, cancel := context.WithTimeout(ctx, chattersRPCTimeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	requestID := nuid.Next()
	req := manage.ChattersRequest{BroadcasterID: strconv.FormatUint(id, 10), RequestID: requestID, WindowID: state.window, SessionGeneration: state.generation, LiveSession: state.liveSession, Cursor: state.cursor, CheckLive: checkLive, DeadlineUnixMilli: deadline.UnixMilli()}
	body, err := codec.Marshal(req)
	if err != nil {
		return manage.ChattersReply{}, err
	}
	msg, err := s.request(ctx, s.chattersSubject, body)
	if err != nil {
		return manage.ChattersReply{}, err
	}
	var reply manage.ChattersReply
	if err := codec.Unmarshal(msg.Data, &reply); err != nil {
		return reply, err
	}
	return reply, validateChatterReply(req, reply, s.now())
}

type chatterCorrelation struct{ broadcaster, request, window, generation, session string }

func requestCorrelation(req manage.ChattersRequest) chatterCorrelation {
	return chatterCorrelation{req.BroadcasterID, req.RequestID, req.WindowID, req.SessionGeneration, req.LiveSession}
}
func replyCorrelation(reply manage.ChattersReply) chatterCorrelation {
	return chatterCorrelation{reply.BroadcasterID, reply.RequestID, reply.WindowID, reply.SessionGeneration, reply.LiveSession}
}
func validateChatterReply(req manage.ChattersRequest, reply manage.ChattersReply, now time.Time) error {
	if replyCorrelation(reply) != requestCorrelation(req) {
		return errors.New("chatter reply correlation mismatch")
	}
	if len(reply.Chatters) > 1000 {
		return errors.New("chatter page exceeds limit")
	}
	if err := validateLiveProofTime(reply, now); err != nil {
		return err
	}
	return validateLiveProofIdentity(reply)
}
func validateLiveProofTime(reply manage.ChattersReply, now time.Time) error {
	if reply.CheckedAtUnixMilli > now.Add(time.Minute).UnixMilli() {
		return errors.New("chatter live confirmation timestamp is in the future")
	}
	if reply.CheckedAtUnixMilli == 0 {
		return nil
	}
	if reply.CheckedAtUnixMilli < now.Add(-time.Minute).UnixMilli() {
		return errors.New("chatter live confirmation timestamp is stale")
	}
	return nil
}
func validateLiveProofIdentity(reply manage.ChattersReply) error {
	if reply.CheckedAtUnixMilli == 0 {
		return nil
	}
	if !reply.Live {
		return nil
	}
	if reply.StreamID == "" || len(reply.StreamID) > 128 {
		return errors.New("chatter live confirmation omitted a valid stream identity")
	}
	if reply.StreamStartedAtUnixMilli <= 0 {
		return errors.New("chatter live confirmation omitted a valid stream identity")
	}
	if reply.StreamStartedAtUnixMilli > reply.CheckedAtUnixMilli {
		return errors.New("chatter live confirmation omitted a valid stream identity")
	}
	return nil
}

func (s *ValkeyLoyaltyClock) chatterViewerID(raw string) (uint64, bool) {
	if raw == s.botID {
		return 0, false
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	return id, err == nil && id != 0
}

type chattersError struct {
	message      string
	missingScope bool
}

func (e *chattersError) Error() string {
	if e.missingScope {
		return "chatters unavailable (missing scope or moderator seat): " + e.message
	}
	return e.message
}
