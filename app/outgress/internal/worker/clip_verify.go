package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// Create Clip acks before processing finishes, and a clip can die in
// processing after the 2xx: the posted link is then permanently dead and
// Twitch sends no error. Twitch's own guidance is to poll Get Clips and treat
// a clip that has not appeared within ~15 seconds of creation as failed. The
// chat reply has already been posted by then, so the check runs detached from
// the lane and only speaks up on confirmed absence.
const (
	clipVerifyDelay   = 16 * time.Second // first Get Clips poll, past Twitch's 15s window
	clipVerifyRecheck = 5 * time.Second  // gap before the confirming re-poll
	clipVerifyTimeout = 30 * time.Second // hard bound on one whole background check
	// clipVerifySlots bounds the in-flight checks per lane worker. Clips are
	// throttled to one per channel per 15s and a check lives under 30s, so the
	// bound only trips under abnormal load — and tripping merely skips the
	// failure notice, never the clip or its reply.
	clipVerifySlots = 16
)

// scheduleClipVerify starts the background publication check for a clip whose
// URL was just posted to chat. Detached from the lane context on purpose: the
// lane acked the message when the clip was created, and holding a lane routine
// through the publication window is exactly what processClip avoids.
func (w *Worker) scheduleClipVerify(broadcasterID, clipper, clipID string) {
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
		w.verifyClipPublished(ctx, broadcasterID, clipper, clipID)
	}()
}

// verifyClipPublished waits out Twitch's publication window and posts the
// failure notice if the clip is confirmed absent. A live clip needs no action:
// the link already posted is the final link.
func (w *Worker) verifyClipPublished(ctx context.Context, broadcasterID, clipper, clipID string) {
	if !w.clipConfirmedAbsent(ctx, broadcasterID, clipID, clipVerifyDelay, clipVerifyRecheck) {
		return
	}
	w.log.Warn("clip never published; posting failure notice",
		zap.String("broadcaster_id", broadcasterID), zap.String("clip_id", clipID))
	if err := w.sendBotChat(ctx, broadcasterID, clipFailedText(clipper)); err != nil {
		w.log.Warn("clip failure notice not sent",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

// clipConfirmedAbsent reports whether Get Clips cleanly said "no such clip" on
// two polls past the publication window. Absence must be proven twice so one
// slow publish or read blip cannot fabricate a failure notice for a clip that
// is actually fine; anything indeterminate stays silent.
func (w *Worker) clipConfirmedAbsent(ctx context.Context, broadcasterID, clipID string, first, recheck time.Duration) bool {
	return w.clipAbsentAfter(ctx, broadcasterID, clipID, first) &&
		w.clipAbsentAfter(ctx, broadcasterID, clipID, recheck)
}

// clipAbsentAfter sleeps, then reports whether one clean Get Clips lookup said
// the clip does not exist. Any error (denied bucket, transport, non-200,
// context end) counts as "not absent": only proven absence may notify.
func (w *Worker) clipAbsentAfter(ctx context.Context, broadcasterID, clipID string, wait time.Duration) bool {
	if !sleepCtx(ctx, wait) {
		return false
	}
	absent, err := w.clipAbsent(ctx, broadcasterID, clipID)
	if err != nil {
		w.log.Warn("clip verify poll failed; staying silent",
			zap.String("broadcaster_id", broadcasterID),
			zap.String("clip_id", clipID), zap.Error(err))
		return false
	}
	return absent
}

// clipAbsent performs one Get Clips lookup by id under the app token, paying
// the app Helix bucket like any other read. (true, nil) means a clean 200
// whose data carries no clip — Twitch does not know the id.
func (w *Worker) clipAbsent(ctx context.Context, broadcasterID, clipID string) (bool, error) {
	if err := w.takeGeneralHelix(ctx, &outgress.Message{As: outgress.AsApp}); err != nil {
		return false, err
	}
	res, err := w.callTwitch(ctx, twitch.IdentityApp, broadcasterID,
		twitch.HelixCall{Method: http.MethodGet, Endpoint: "/helix/clips?id=" + url.QueryEscape(clipID)})
	if err != nil {
		return false, err
	}
	defer drainResponse(res)
	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("get clips: %d", res.StatusCode)
	}
	var reply clipCreateReply
	if err := json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&reply); err != nil {
		return false, err
	}
	return len(reply.Data) == 0, nil
}

// clipFailedText composes the follow-up chat line for a clip Twitch acked but
// never published. It names the clipper when known, so they see why their link
// is dead and know a retry is worth it.
func clipFailedText(clipper string) string {
	notice := "the clip didn't make it through Twitch's processing, so the link won't work. Try !clip again."
	if clipper == "" {
		return "Heads up: " + notice
	}
	return "@" + clipper + " " + notice
}
