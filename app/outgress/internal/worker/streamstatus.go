package worker

import (
	"context"
	"errors"
	"strconv"
	"time"

	eventtwitch "ItsBagelBot/internal/domain/event/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"

	"github.com/bytedance/sonic"

	"go.uber.org/zap"
)

// processStreamStatus resolves one broadcaster's live state from Twitch (Helix
// Get Streams) and writes it back into the live projection. It pays the reserved
// system Helix bucket and runs only on the system lane (where SetLiveWriter has
// attached the write-back). A permanent Twitch rejection is dropped; transient
// errors nack so the paced redelivery retries.
func (w *Worker) processStreamStatus(ctx context.Context, payload outgress.Message) error {
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

	isLive, err := w.twitch.IsStreamLive(ctx, payload.BroadcasterID)
	if err != nil {
		return w.streamStatusFailure(ctx, payload.BroadcasterID, err)
	}

	if err := w.live.Write(ctx, payload.BroadcasterID, isLive); err != nil {
		return err
	}

	if isLive {
		// Proactively re-verify in the background when a channel goes live.
		w.scheduleModStatus(payload.BroadcasterID, payload.SenderID)
	}

	w.log.Debug("stream_status resolved",
		zap.String("broadcaster_id", payload.BroadcasterID), zap.Bool("live", isLive))
	return nil
}

// seedLiveStatus resolves the broadcaster's current live state right after an
// EventSub enroll. Twitch only delivers stream.online for sessions that start
// after the subscription exists, so a channel enrolled (or re-enrolled) while
// its stream is already running never receives the go-live event for the
// session in progress; without this seed the live projection stays cold and
// every live-gated command reads offline until the next stream. Best-effort:
// the enroll itself already succeeded, and the worker's cold-miss escalation
// remains the safety net when the seed fails.
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

// streamStatusFailure drops permanent Twitch rejections (retrying can never
// fix them) and nacks the rest so the paced redelivery retries.
func (w *Worker) streamStatusFailure(ctx context.Context, broadcasterID string, err error) error {
	if isPermanent(err) {
		w.log.Error("dropping stream_status twitch rejected",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		noticeError(ctx, err)
		return nil
	}

	w.log.Warn("stream_status check failed, will retry",
		zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	return err
}

// HandleStreamEvent reacts to a real Twitch stream.online / stream.offline
// EventSub message off the ingress stream lane (env NATS_SUBJECT_LANE_STREAM).
//
// Background: the worker fleet escalates a cold live query to the system lane's
// stream_status path, which re-verifies the bot's mod status as a side effect.
// Once stream.online events flow and the projector writes the live key directly,
// that live query is no longer cold, so the escalation (and its mod-status
// re-verify) never runs. This handler restores the re-verify by reacting to the
// real go-live event itself.
//
// It is bound under outgress's OWN durable group (separate from the projector's),
// so every event is delivered here once in addition to the projector's copy. It
// does NOT write live state (that is the projector's job); it only re-verifies
// mod status, best-effort. Decoding is shared with the projector via the domain
// stream_status decoder. Always acks (returns nil): a re-verify is advisory and
// must never poison or replay the lane.
func (w *Worker) HandleStreamEvent(msg *bus.Message) error {
	status, ok := eventtwitch.DecodeStreamStatus(msg.Payload)
	if !ok {
		// Not a stream.online/offline we understand (or malformed). Ack and move
		// on; the decoder already rejects everything but those two types.
		return nil
	}

	// Only go-live triggers the re-verify; an offline event needs no mod check.
	if !status.Live {
		return nil
	}

	broadcasterID := strconv.FormatUint(status.BroadcasterID, 10)

	w.scheduleModStatus(broadcasterID, "")
	w.reauthBeaconOnLive(msg.Context(), broadcasterID)

	w.log.Debug("mod status refresh scheduled on go-live",
		zap.String("broadcaster_id", broadcasterID))
	return nil
}

// reauthBeaconTTL spaces the go-live reconnect nudge: one chat line per
// channel per window, however many times the stream restarts.
const reauthBeaconTTL = 12 * time.Hour

// reauthBeaconOnLive asks the streamer to reconnect, in their own chat, when
// they go live on a channel whose authorization Twitch revoked. stream.online
// survives a revocation (it needs no user grant) and chat send runs on the
// app token backed by the bot's own user:bot grant plus its moderator seat,
// so this path stays alive exactly when everything scoped is dead. That makes
// go-live the one reliable moment the bot can still reach the streamer where
// they are looking. Best-effort at every step; the beacon must never disturb
// the go-live pipeline.
func (w *Worker) reauthBeaconOnLive(ctx context.Context, broadcasterID string) {
	if w.reauth == nil {
		return
	}
	ch, found, err := w.registry.Get(ctx, broadcasterID)
	if err != nil || !found {
		return
	}
	if ch.SubState != subStateRevoked {
		return
	}

	armed, err := w.registry.ArmReauthBeacon(ctx, broadcasterID, reauthBeaconTTL)
	if err != nil || !armed {
		return
	}

	if err := w.sendReauthChat(ctx, broadcasterID); err != nil {
		w.log.Warn("reauth chat beacon failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	w.log.Info("reauth chat beacon sent", zap.String("broadcaster_id", broadcasterID))
}

// sendReauthChat pushes the localized reconnect line through the ordinary
// chat send path (type route defaults + bot sender injection + per-channel
// chat rate bucket), exactly as if a lane job carried it.
func (w *Worker) sendReauthChat(ctx context.Context, broadcasterID string) error {
	body, err := sonic.Marshal(struct {
		BroadcasterID string `json:"broadcaster_id"`
		Message       string `json:"message"`
	}{broadcasterID, w.reauth.ChatBeacon(ctx, broadcasterID)})
	if err != nil {
		return err
	}

	payload := outgress.Message{
		Type:          outgress.TypeChat,
		BroadcasterID: broadcasterID,
		Payload:       body,
	}
	if !w.resolveHelixRoute(&payload) {
		return errReauthChatRoute
	}
	return w.processChat(ctx, payload)
}

// errReauthChatRoute only fires if the chat type route table loses its "chat"
// entry, which would be a programming error.
var errReauthChatRoute = errors.New("reauth beacon: chat route unresolved")
