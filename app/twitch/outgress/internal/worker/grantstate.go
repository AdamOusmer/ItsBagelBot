// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"net/http"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"

	"go.uber.org/zap"
)

type grantRegistry interface {
	Get(ctx context.Context, broadcasterID string) (manage.Channel, bool, error)
	SetGrantState(ctx context.Context, broadcasterID string, state manage.GrantState) error
}

func (w *Worker) callTwitch(ctx context.Context, id twitch.Identity, broadcasterID string, call twitch.HelixCall) (*http.Response, error) {
	res, err := w.twitch.ExecuteAs(ctx, id, broadcasterID, call)
	w.noteGrantHealth(ctx, id, broadcasterID, err)
	return res, err
}

func (w *Worker) noteGrantHealth(ctx context.Context, id twitch.Identity, broadcasterID string, err error) {
	if !w.grantTrackable(id, broadcasterID) {
		return
	}
	if twitch.GrantDead(err) {
		w.setGrantState(ctx, broadcasterID, manage.GrantDead)
		return
	}
	if err == nil {
		w.setGrantState(ctx, broadcasterID, manage.GrantUnknown)
	}
}

func (w *Worker) grantTrackable(id twitch.Identity, broadcasterID string) bool {
	return id == twitch.IdentityBroadcaster && broadcasterID != "" && w.grants != nil
}

func (w *Worker) currentGrantState(ctx context.Context, broadcasterID string) (manage.GrantState, bool) {
	ch, found, err := w.grants.Get(ctx, broadcasterID)
	if err != nil || !found {
		return manage.GrantUnknown, false
	}
	return ch.GrantState, true
}

func (w *Worker) setGrantState(ctx context.Context, broadcasterID string, state manage.GrantState) {
	current, ok := w.currentGrantState(ctx, broadcasterID)
	if !ok {
		return
	}
	if current == state {
		return
	}

	if err := w.grants.SetGrantState(ctx, broadcasterID, state); err != nil {
		w.log.Warn("grant state write failed",
			zap.String("broadcaster_id", broadcasterID),
			zap.String("grant_state", string(state)),
			zap.Error(err))
		return
	}

	w.log.Info("grant state changed",
		zap.String("broadcaster_id", broadcasterID),
		zap.String("from", string(current)),
		zap.String("to", string(state)))

	w.notifyGrantDead(ctx, broadcasterID, state)
}

func (w *Worker) clearGrantDead(ctx context.Context, broadcasterID string, ch manage.Channel) {
	if w.grants == nil || ch.GrantState != manage.GrantDead {
		return
	}
	if err := w.grants.SetGrantState(ctx, broadcasterID, manage.GrantUnknown); err != nil {
		w.log.Warn("clearing grant state failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	w.log.Info("grant restored by re-consent", zap.String("broadcaster_id", broadcasterID))
}

func (w *Worker) notifyGrantDead(ctx context.Context, broadcasterID string, state manage.GrantState) {
	if state != manage.GrantDead || w.reauth == nil {
		return
	}
	w.reauth.Notify(ctx, broadcasterID, noticeGrantDead)
}
