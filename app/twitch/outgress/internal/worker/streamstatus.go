// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	eventtwitch "ItsBagelBot/internal/domain/event/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func (w *Worker) processStreamStatus(ctx context.Context, payload *outgress.Message) error {
	if w.live == nil {
		w.log.Error("dropping stream_status job off the system lane")
		return nil
	}
	if payload.BroadcasterID == "" {
		w.log.Error("dropping stream_status job without broadcaster id")
		return nil
	}

	if err := w.takeSystemHelix(ctx); err != nil {
		return err
	}

	details, isLive, err := w.twitch.StreamDetails(ctx, payload.BroadcasterID)
	if err != nil {
		return w.streamStatusFailure(ctx, payload.BroadcasterID, err)
	}

	if err := w.live.Write(ctx, payload.BroadcasterID, isLive); err != nil {
		return w.streamStatusFailure(ctx, payload.BroadcasterID, err)
	}

	w.persistStreamInfo(ctx, payload.BroadcasterID, isLive, details)

	if isLive {
		w.scheduleModStatus(payload.BroadcasterID, payload.SenderID)
	}

	w.log.Debug("stream_status resolved",
		zap.String("broadcaster_id", payload.BroadcasterID), zap.Bool("live", isLive))
	return nil
}

func (w *Worker) persistStreamInfo(ctx context.Context, broadcasterID string, isLive bool, details twitch.StreamDetails) {
	if w.streamInfo == nil {
		return
	}

	prev, _, err := w.streamInfo.GetStreamInfo(ctx, broadcasterID)
	if err != nil {
		w.log.Warn("stream info: prior read failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	}

	info := nextStreamInfo(prev, isLive, details)
	if err := w.streamInfo.SetStreamInfo(ctx, broadcasterID, info); err != nil {
		w.log.Warn("stream info: projection write failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

func nextStreamInfo(prev projection.StreamInfo, isLive bool, details twitch.StreamDetails) projection.StreamInfo {
	if !isLive {
		prev.EndedAt = time.Now().UTC()
		return prev
	}

	prev.Title = details.Title
	prev.GameName = details.GameName
	prev.ViewerCount = details.ViewerCount
	prev.EndedAt = time.Time{}
	if details.ViewerCount > prev.PeakViewers {
		prev.PeakViewers = details.ViewerCount
	}
	if !details.StartedAt.IsZero() {
		prev.StartedAt = details.StartedAt
	}
	return prev
}

func (w *Worker) seedLiveStatus(ctx context.Context, broadcasterID string) {
	if w.live == nil {
		return
	}
	if err := w.takeSystemHelix(ctx); err != nil {
		w.log.Warn("live seed: no system budget, skipping",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	isLive, err := w.twitch.IsStreamLive(ctx, broadcasterID)
	if err != nil {
		w.log.Warn("live seed: stream check failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	if err := w.live.Write(ctx, broadcasterID, isLive); err != nil {
		w.log.Warn("live seed: projection write failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	if isLive {
		w.scheduleModStatus(broadcasterID, "")
	}
	w.log.Info("live state seeded after enroll",
		zap.String("broadcaster_id", broadcasterID), zap.Bool("live", isLive))
}

func (w *Worker) streamStatusFailure(ctx context.Context, broadcasterID string, err error) error {
	reason, drop := streamStatusDrop(err)
	if drop {
		w.log.Error(reason,
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		noticeError(ctx, err)
		return nil
	}

	w.log.Warn("stream_status check failed, will retry",
		zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	return err
}

func streamStatusDrop(err error) (string, bool) {
	var verr *valkey.ValkeyError
	switch {
	case isPermanent(err):
		return "dropping stream_status twitch rejected", true
	case errors.As(err, &verr):
		return "dropping stream_status valkey rejected the live write", true
	default:
		return "", false
	}
}

func (w *Worker) HandleStreamEvent(msg *bus.Message) error {
	log := monitor.TxnLogger(msg.Context(), w.log)
	status, ok := eventtwitch.DecodeStreamStatus(msg.Payload)
	if !ok {
		return nil
	}

	if !status.Live {
		return nil
	}

	broadcasterID := strconv.FormatUint(status.BroadcasterID, 10)

	w.scheduleModStatus(broadcasterID, "")
	w.reauthBeaconOnLive(msg.Context(), broadcasterID)

	log.Debug("mod status refresh scheduled on go-live",
		zap.String("broadcaster_id", broadcasterID))
	return nil
}

const reauthBeaconTTL = 12 * time.Hour

func (w *Worker) reauthBeaconOnLive(ctx context.Context, broadcasterID string) {
	if w.reauth == nil {
		return
	}
	ch, found, err := w.registry.Get(ctx, broadcasterID)
	if err != nil || !found {
		return
	}
	n, ok := liveNotice(ch)
	if !ok {
		return
	}

	armed, err := w.registry.ArmReauthBeacon(ctx, broadcasterID, reauthBeaconTTL)
	if err != nil || !armed {
		return
	}

	locale := w.reauth.ResolveLocale(ctx, broadcasterID)
	w.reauth.NotifyLocalized(ctx, broadcasterID, locale, n)

	if err := w.sendReauthChat(ctx, broadcasterID, locale, n); err != nil {
		w.log.Warn("reauth chat beacon failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	w.log.Info("reauth chat beacon sent",
		zap.String("broadcaster_id", broadcasterID),
		zap.String("reason", n.request))
}

func liveNotice(ch manage.Channel) (notice, bool) {
	switch {
	case ch.SubState == subStateRevoked:
		return noticeRevoked, true
	case ch.SubState == subStateBanned:
		return noticeBanned, true
	case ch.GrantState == manage.GrantDead:
		return noticeGrantDead, true
	default:
		return notice{}, false
	}
}

func (w *Worker) sendReauthChat(ctx context.Context, broadcasterID, locale string, n notice) error {
	if n.chat == "" {
		return nil
	}
	return w.sendBotChat(ctx, broadcasterID, w.reauth.ChatLine(locale, n))
}
