// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

const (
	clipVerifyDelay   = 16 * time.Second
	clipVerifyRecheck = 5 * time.Second
	clipVerifyTimeout = 30 * time.Second
	clipVerifySlots   = 16
)

func (w *Worker) scheduleClipVerify(broadcasterID, clipper, clipID string) {
	probe := clipProbe{broadcasterID: broadcasterID, clipper: clipper, clipID: clipID}
	select {
	case w.clipVerify <- struct{}{}:
	default:
		w.log.Warn("clip verify skipped: checks saturated",
			zap.String("broadcaster_id", broadcasterID), zap.String("clip_id", clipID))
		return
	}
	go func() {
		defer func() { <-w.clipVerify }()
		ctx, cancel := context.WithTimeout(context.Background(), clipVerifyTimeout)
		defer cancel()
		w.verifyClipPublished(ctx, probe)
	}()
}

type clipProbe struct {
	broadcasterID string
	clipper       string
	clipID        string
}

func (w *Worker) verifyClipPublished(ctx context.Context, probe clipProbe) {
	if !w.clipConfirmedAbsent(ctx, probe, clipVerifyDelay, clipVerifyRecheck) {
		return
	}
	w.log.Warn("clip never published; posting failure notice",
		zap.String("broadcaster_id", probe.broadcasterID), zap.String("clip_id", probe.clipID))
	if err := w.sendBotChat(ctx, probe.broadcasterID, clipFailedText(probe.clipper)); err != nil {
		w.log.Warn("clip failure notice not sent",
			zap.String("broadcaster_id", probe.broadcasterID), zap.Error(err))
	}
}

func (w *Worker) clipConfirmedAbsent(ctx context.Context, probe clipProbe, first, recheck time.Duration) bool {
	return w.clipAbsentAfter(ctx, probe, first) &&
		w.clipAbsentAfter(ctx, probe, recheck)
}

func (w *Worker) clipAbsentAfter(ctx context.Context, probe clipProbe, wait time.Duration) bool {
	if !sleepCtx(ctx, wait) {
		return false
	}
	absent, err := w.clipAbsent(ctx, probe)
	if err != nil {
		w.log.Warn("clip verify poll failed; staying silent",
			zap.String("broadcaster_id", probe.broadcasterID),
			zap.String("clip_id", probe.clipID), zap.Error(err))
		return false
	}
	return absent
}

func (w *Worker) clipAbsent(ctx context.Context, probe clipProbe) (bool, error) {
	if err := w.takeGeneralHelix(ctx, &outgress.Message{As: outgress.AsApp}); err != nil {
		return false, err
	}
	res, err := w.callTwitch(ctx, twitch.IdentityApp, probe.broadcasterID,
		twitch.HelixCall{Method: http.MethodGet, Endpoint: "/helix/clips?id=" + url.QueryEscape(probe.clipID)})
	if err != nil {
		return false, err
	}
	defer drainResponse(res)
	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("get clips: %d", res.StatusCode)
	}
	var reply clipCreateReply
	if err := codec.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&reply); err != nil {
		return false, err
	}
	return len(reply.Data) == 0, nil
}

func clipFailedText(clipper string) string {
	notice := "the clip didn't make it through Twitch's processing, so the link won't work. Try !clip again."
	if clipper == "" {
		return "Heads up: " + notice
	}
	return "@" + clipper + " " + notice
}
