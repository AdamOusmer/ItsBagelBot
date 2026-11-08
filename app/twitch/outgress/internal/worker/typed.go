// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"strings"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

type typedJob struct {
	as   string
	call func(ctx context.Context) error
}

func (w *Worker) runTyped(ctx context.Context, payload *outgress.Message, job typedJob) error {
	payload.As = job.as
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}
	guarded, err := w.channelAttemptContext(ctx, payload.BroadcasterID)
	if err != nil {
		return err
	}
	err = job.call(guarded)
	w.noteGrantHealth(ctx, twitch.ParseIdentity(job.as), payload.BroadcasterID, err)
	if err != nil {
		return w.typedFailure(ctx, payload, err)
	}
	return nil
}

func (w *Worker) typedFailure(ctx context.Context, payload *outgress.Message, err error) error {
	if !typedDrop(err) {
		w.log.Warn("twitch action failed, will retry",
			zap.String("type", payload.Type),
			zap.String("broadcaster_id", payload.BroadcasterID), zap.Error(err))
		return err
	}
	w.log.Error("dropping twitch action: twitch rejected it",
		zap.String("type", payload.Type),
		zap.String("broadcaster_id", payload.BroadcasterID), zap.Error(err))
	noticeError(ctx, err)
	return nil
}

func typedDrop(err error) bool {
	return streamDrop(err) || errors.Is(err, twitch.ErrNoSnoozes)
}

func (w *Worker) decodeTyped(payload *outgress.Message, into any) bool {
	if err := codec.Unmarshal(payload.Payload, into); err != nil {
		w.log.Error("dropping twitch action: undecodable payload",
			zap.String("type", payload.Type),
			zap.String("broadcaster_id", payload.BroadcasterID), zap.Error(err))
		return false
	}
	return true
}

func (w *Worker) resolveLogin(ctx context.Context, payload *outgress.Message, login string) (string, error) {
	login = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(login), "@"))
	if login == "" {
		w.log.Error("dropping twitch action: no target login",
			zap.String("type", payload.Type),
			zap.String("broadcaster_id", payload.BroadcasterID))
		return "", nil
	}
	id, err := w.userIDs.GetOrLoad(ctx, "login:"+login, func(ctx context.Context) (string, error) {
		return w.resolveTypedLogin(ctx, payload, login)
	})
	if err != nil {
		w.log.Warn("twitch action target resolve failed, will retry",
			zap.String("type", payload.Type),
			zap.String("broadcaster_id", payload.BroadcasterID), zap.Error(err))
		return "", err
	}
	if id == "" {
		w.log.Warn("dropping twitch action: no such target user",
			zap.String("type", payload.Type),
			zap.String("broadcaster_id", payload.BroadcasterID))
	}
	return id, nil
}

func (w *Worker) resolveTypedLogin(ctx context.Context, payload *outgress.Message, login string) (string, error) {
	guarded, err := w.channelAttemptContext(ctx, payload.BroadcasterID)
	if err != nil {
		return "", err
	}
	if err := w.takeAppHelix(guarded); err != nil {
		return "", err
	}
	return w.twitch.UserIDByLogin(guarded, login)
}
