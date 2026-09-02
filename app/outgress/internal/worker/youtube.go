// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/outgress/internal/youtube"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

// YouTube actions spend the project's daily quota budget, which no amount of
// pacing refills: one chat line is 50 units of a ~10k/day budget. So every
// handler pays two gates before its call — a per-chat send-pacing bucket
// (YouTube throttles bots per chat far harder than Twitch's 20/30s) and the
// fleet-shared daily ledger — and classifies results into drop (nil) versus
// nack (error) exactly like the Helix paths.
const (
	// ytChatPacingCapacity/Refill: one message per interval per chat. Google's
	// live chat insert throttles around one bot message every few seconds;
	// six seconds keeps commands readable and never trips it. The interval is
	// env-tunable (YOUTUBE_CHAT_MIN_INTERVAL_MS) via setYouTubePacing at
	// wiring time.
	ytChatPacingCapacity = 1.0
	ytChatDefaultWindow  = 6.0

	// ytChatMaxBytes bounds chat text past this many bytes as malformed or
	// abusive; a well-formed YouTube chat line is far shorter than this.
	ytChatMaxBytes = 2_048
)

// ytChatPacingSpec is rebuilt once at wiring from YOUTUBE_CHAT_MIN_INTERVAL_MS
// (setYouTubePacing); the var keeps the default so tests need no wiring.
var ytChatPacingSpec = ratelimit.NewSpec(ytChatPacingCapacity, ytChatPacingCapacity/ytChatDefaultWindow)

// ytBudget narrows the daily-quota ledger so tests can fake it without
// Valkey.
type ytBudget interface {
	Take(ctx context.Context, cost int64) (bool, error)
}

// youtubeAPI is the slice of the Data API the handlers fire; *youtube.Client
// satisfies it in production and a recorder stands in for tests.
type youtubeAPI interface {
	SendChatMessage(ctx context.Context, liveChatID, text string) error
	DeleteChatMessage(ctx context.Context, msgID string) error
	Ban(ctx context.Context, liveChatID, targetChannelID string) error
	Timeout(ctx context.Context, liveChatID, targetChannelID string, durationSeconds int64) error
}

// processYouTubeChat sends one chat line into the broadcaster's active live
// chat. The enabled/disabled decision belongs upstream (sesame), mirroring
// the Twitch chat path; outgress only gates, resolves the target chat, and
// fires.
func (w *Worker) processYouTubeChat(ctx context.Context, payload *outgress.Message) error {
	text, ok := decodeYouTubeText(w.log, payload)
	if !ok {
		return nil
	}

	chatID, ok := w.resolveYouTubeChat(payload)
	if !ok {
		return nil
	}

	// Pacing first: a nack here retries later and must not burn today's
	// quota. Budget second: a refusal drops (no retry) because Google's
	// day will not refill. The original order charged 50 units then nacked
	// on pacing, so every redelivery spent another 50 of a 10k/day budget.
	if err := w.take(ctx, ytChatPacingSpec.ForDynamicKey(
		"ratelimit:yt:chat:", "yt:chat", payload.BroadcasterID)); err != nil {
		return err
	}
	admitted, err := w.admitYouTubeBudget(ctx, payload)
	if err != nil || !admitted {
		return err
	}

	return w.classifyYouTubeResult(ctx, payload,
		w.youtube.SendChatMessage(youtube.WithChannel(ctx, payload.BroadcasterID), chatID, text))
}

// processYouTubeDelete removes one chat message by id (Message.MsgID). No
// pacing bucket: moderation is not bot chatter, but the daily budget still
// applies.
func (w *Worker) processYouTubeDelete(ctx context.Context, payload *outgress.Message) error {
	if payload.MsgID == "" {
		w.log.Error("dropping youtube delete: no message id",
			zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}

	admitted, err := w.admitYouTubeBudget(ctx, payload)
	if err != nil || !admitted {
		return err
	}

	return w.classifyYouTubeResult(ctx, payload,
		w.youtube.DeleteChatMessage(youtube.WithChannel(ctx, payload.BroadcasterID), payload.MsgID))
}

// processYouTubeBan / processYouTubeTimeout issue liveChatBans.insert against
// Message.To's channel id. Timeout shares ban's endpoint; the type plus
// banDurationSeconds make it temporary. There is deliberately no unban: it
// needs the ban resource id returned at creation, which outgress does not
// persist.
func (w *Worker) processYouTubeBan(ctx context.Context, payload *outgress.Message) error {
	return w.processYouTubeModeration(ctx, payload, false)
}

func (w *Worker) processYouTubeTimeout(ctx context.Context, payload *outgress.Message) error {
	return w.processYouTubeModeration(ctx, payload, true)
}

func (w *Worker) processYouTubeModeration(ctx context.Context, payload *outgress.Message, temporary bool) error {
	target := payload.To
	if target == "" {
		w.log.Error("dropping youtube moderation: no target channel id",
			zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}

	var body struct {
		Reason          string `json:"reason"`
		DurationSeconds int64  `json:"duration_seconds"`
	}
	if len(payload.Payload) > 0 {
		if err := codec.Unmarshal(payload.Payload, &body); err != nil {
			w.dropMalformedYouTube("moderation", payload, err)
			return nil
		}
	}

	chatID, ok := w.resolveYouTubeChat(payload)
	if !ok {
		return nil
	}

	admitted, err := w.admitYouTubeBudget(ctx, payload)
	if err != nil || !admitted {
		return err
	}

	if temporary {
		return w.classifyYouTubeResult(ctx, payload,
			w.youtube.Timeout(youtube.WithChannel(ctx, payload.BroadcasterID), chatID, target, body.DurationSeconds))
	}
	return w.classifyYouTubeResult(ctx, payload,
		w.youtube.Ban(youtube.WithChannel(ctx, payload.BroadcasterID), chatID, target))
}

// resolveYouTubeChat picks the target chat: an explicit producer-supplied id
// wins, else the directory cache learned from lifecycle events. A miss drops
// loudly instead of discovering over the Data API — see ChatDirectory for why
// there is no discovery fallback.
func (w *Worker) resolveYouTubeChat(payload *outgress.Message) (string, bool) {
	if payload.LiveChatID != "" {
		return payload.LiveChatID, true
	}

	chatID := w.ytChats.Get(payload.BroadcasterID)
	if chatID == "" {
		w.log.Warn("dropping youtube action: no known live chat (watcher has not seen this channel go live)",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("type", payload.Type))
		return "", false
	}
	return chatID, true
}

// admitYouTubeBudget charges one action (50 units) against the daily ledger
// and reports whether the call may proceed. A refusal means today's quota is
// committed elsewhere; redelivery cannot fix that before tomorrow, so the
// caller drops (false, nil). An infra error returns (false, err) to nack.
func (w *Worker) admitYouTubeBudget(ctx context.Context, payload *outgress.Message) (bool, error) {
	allowed, err := w.ytBudget.Take(ctx, youtube.QuotaUnitsPerAction)
	if err != nil {
		return false, err
	}
	if !allowed {
		w.log.Warn("dropping youtube action: daily quota budget exhausted",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("type", payload.Type))
		noticeError(ctx, errYouTubeQuotaBudget)
		return false, nil
	}
	return true, nil
}

// classifyYouTubeResult maps client errors onto the lane discipline:
//
//	ErrQuotaExhausted / ErrChatEnded / ErrChatNotFound / ErrAuth -> drop
//	ErrRateLimited, network errors, anything else               -> nack
func (w *Worker) classifyYouTubeResult(ctx context.Context, payload *outgress.Message, err error) error {
	switch {
	case err == nil:
		return nil

	case isYouTubePermanent(err):
		w.log.Error("dropping youtube action (permanent)",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("type", payload.Type),
			zap.Error(err))
		noticeError(ctx, err)
		return nil

	default:
		w.log.Warn("youtube action failed, will retry",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("type", payload.Type),
			zap.Error(err))
		return err
	}
}

func isYouTubePermanent(err error) bool {
	return errors.Is(err, youtube.ErrQuotaExhausted) ||
		errors.Is(err, youtube.ErrChatEnded) ||
		errors.Is(err, youtube.ErrChatNotFound) ||
		errors.Is(err, youtube.ErrAuth) ||
		errors.Is(err, youtube.ErrNoChannelInContext)
}

// decodeYouTubeText extracts {"message": text} from a youtube_chat payload.
func decodeYouTubeText(log *zap.Logger, payload *outgress.Message) (string, bool) {
	var body struct {
		Message string `json:"message"`
	}
	if err := codec.Unmarshal(payload.Payload, &body); err != nil {
		log.Error("dropping youtube chat: malformed payload",
			zap.String("broadcaster_id", payload.BroadcasterID), zap.Error(err))
		return "", false
	}
	if body.Message == "" || len(body.Message) > ytChatMaxBytes {
		log.Error("dropping youtube chat: empty or oversized text",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.Int("bytes", len(body.Message)))
		return "", false
	}
	return body.Message, true
}

func (w *Worker) dropMalformedYouTube(action string, payload *outgress.Message, err error) {
	w.log.Error("dropping youtube "+action+": malformed payload",
		zap.String("broadcaster_id", payload.BroadcasterID), zap.Error(err))
}

var errYouTubeQuotaBudget = errorString("youtube daily quota budget refused admission")

type errorString string

func (e errorString) Error() string { return string(e) }

// SetYouTubePacing retunes the per-chat pacing bucket from configuration.
// Wiring calls it once before any consumer starts; specs are pre-encoded, so
// this rebuilds the package-level var rather than mutating it.
func SetYouTubePacing(interval time.Duration) {
	ytChatPacingSpec = ratelimit.NewSpec(ytChatPacingCapacity, ytChatPacingCapacity/interval.Seconds())
}
