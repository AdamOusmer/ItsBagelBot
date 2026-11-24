// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	catalog "ItsBagelBot/internal/domain/analytics"
	eventtwitch "ItsBagelBot/internal/domain/event/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func (w *Worker) processStreamStatus(ctx context.Context, payload *outgress.Message) error {
	if allowed, err := w.admitPayload(ctx, payload); err != nil || !allowed {
		return err
	}

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

	return w.publishStreamStatus(ctx, payload, details, isLive)
}
func (w *Worker) publishStreamStatus(ctx context.Context, payload *outgress.Message, details twitch.StreamDetails, isLive bool) error {
	if allowed, err := w.admitPayload(ctx, payload); err != nil || !allowed {
		return err
	}
	if err := w.live.Write(ctx, payload.BroadcasterID, isLive); err != nil {
		return w.streamStatusFailure(ctx, payload.BroadcasterID, err)
	}

	w.persistStreamInfo(ctx, payload.BroadcasterID, isLive, details)

	if isLive {
		w.recordAudienceSize(payload.BroadcasterID, details.ViewerCount)
		w.scheduleModStatus(ctx, payload.BroadcasterID, payload.SenderID)
	}

	w.log.Debug("stream_status resolved",
		zap.String("broadcaster_id", payload.BroadcasterID), zap.Bool("live", isLive))
	return nil
}

func (w *Worker) recordAudienceSize(broadcasterID string, viewers int) {
	id, err := strconv.ParseUint(broadcasterID, 10, 64)
	if err != nil {
		return
	}
	w.analytics.Distinct(catalog.ChannelsSize, id, audienceSizeBucket(viewers))
}

func audienceSizeBucket(viewers int) string {
	switch {
	case viewers < 10:
		return "v0_9"
	case viewers < 50:
		return "v10_49"
	case viewers < 200:
		return "v50_199"
	case viewers < 1000:
		return "v200_999"
	default:
		return "v1kp"
	}
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
	if !w.backgroundAllowed(ctx, broadcasterID) {
		return
	}
	if w.live == nil {
		return
	}
	seed := &outgress.Message{BroadcasterID: broadcasterID}
	if w.stampCurrent(ctx, seed) != nil {
		return
	}
	if w.checkAttemptAdmission(ctx, seed) != nil {
		return
	}
	isLive, err := w.checkLiveSeed(ctx, broadcasterID)
	if err != nil {
		return
	}
	w.publishLiveSeed(ctx, seed, isLive)
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
	ctx := msg.Context()
	status, ok := eventtwitch.DecodeStreamStatus(msg.Payload)
	if !ok || !status.Live {
		return nil
	}

	broadcasterID := strconv.FormatUint(status.BroadcasterID, 10)
	fence, allowed := w.backgroundFence(ctx, broadcasterID)
	if !allowed {
		return nil
	}
	w.scheduleModStatus(ctx, broadcasterID, "")

	log := monitor.TxnLogger(ctx, w.log)
	log.Debug("mod status refresh scheduled on go-live", zap.String("broadcaster_id", broadcasterID))
	w.scheduleScopeNotice(ctx, scopeNoticeTarget{broadcasterID: broadcasterID, fence: fence})
	return nil
}

func (w *Worker) checkLiveSeed(ctx context.Context, broadcasterID string) (bool, error) {
	if err := w.takeSystemHelix(ctx); err != nil {
		w.log.Warn("live seed: no system budget, skipping", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return false, err
	}
	isLive, err := w.twitch.IsStreamLive(ctx, broadcasterID)
	if err != nil {
		w.log.Warn("live seed: stream check failed", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	}
	return isLive, err
}
func (w *Worker) publishLiveSeed(ctx context.Context, seed *outgress.Message, isLive bool) {
	if w.checkAttemptAdmission(ctx, seed) != nil {
		return
	}
	if !w.backgroundAllowed(ctx, seed.BroadcasterID) {
		return
	}
	if err := w.live.Write(ctx, seed.BroadcasterID, isLive); err != nil {
		w.log.Warn("live seed: projection write failed", zap.String("broadcaster_id", seed.BroadcasterID), zap.Error(err))
		return
	}
	if isLive {
		w.scheduleModStatus(ctx, seed.BroadcasterID, "")
	}
	w.log.Info("live state seeded after enroll", zap.String("broadcaster_id", seed.BroadcasterID), zap.Bool("live", isLive))
}
