package worker

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"

	"github.com/bytedance/sonic"
	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

func (w *Worker) Process(msg *bus.Message) error {
	ctx := msg.Context()
	log := monitor.TxnLogger(ctx, w.log)
	processStarted := time.Now()
	defer recordStageDuration(ctx, "outgress.total_ms", processStarted)

	var payload outgress.Message
	if !w.decodePayload(ctx, msg.Payload, &payload) {
		return nil
	}
	annotateTxn(ctx, &payload)

	if err := w.checkPaused(ctx); err != nil {
		return err
	}
	if payload.Type == outgress.TypeBatch {
		var batch outgress.Batch
		if err := decodeBatch(payload.Payload, &batch); err != nil {
			log.Error("dropping malformed outgress batch", zap.Error(err))
			noticeError(ctx, err)
			return nil
		}
		return w.processBatch(ctx, &batch, payload.BroadcasterID, msg.UUID)
	}
	return w.processPayload(ctx, &payload)
}

// processPayload dispatches one decoded, admitted action through the immutable
// action registry: an O(1) lookup by type, the route fill (declared defaults,
// explicit fields win), then the action's Run. Batch jobs call it serially for
// each child; ordinary jobs call it once. Everything before Run is in-process,
// so the only wait a message pays after this point is its own Twitch call.
func (w *Worker) processPayload(ctx context.Context, payload *outgress.Message) error {
	act, ok := w.actions.Lookup(payload.Type)
	if !ok {
		w.log.Error("dropping message with unknown type", zap.String("type", payload.Type))
		return nil
	}
	if !act.FillRoute(payload) {
		w.log.Error("dropping message with invalid request",
			zap.String("type", payload.Type),
			zap.String("endpoint", payload.Endpoint),
			zap.String("method", payload.Method))
		return nil
	}
	return act.Run(ctx, payload)
}

// sendBotLine routes one synthetic bot line, honoring a leading slash-verb
// the same way sesame's pipeline does for module outputs: outgress.CutSlash
// owns the grammar, so "/announce hi" becomes a native announcement, "/pin"
// a pin, "/shoutout <target>" a shoutout, and anything else (including /me,
// which Twitch chat renders itself) a plain chat line. A routed action with
// no usable payload (empty announce/pin message, shoutout without a target)
// is dropped rather than sent for Twitch to reject.
func (w *Worker) sendBotLine(ctx context.Context, broadcasterID, text string) error {
	sc, ok := outgress.CutSlash(text)
	if !ok {
		return w.sendBotChat(ctx, broadcasterID, text)
	}
	switch sc.Type {
	case outgress.TypeShoutout:
		if sc.To == "" {
			return nil
		}
		return w.processPayload(ctx, &outgress.Message{
			Type:          outgress.TypeShoutout,
			BroadcasterID: broadcasterID,
			To:            sc.To,
			Payload:       []byte("{}"),
		})
	case outgress.TypeAnnounce:
		if sc.Text == "" {
			return nil
		}
		body, err := sonic.Marshal(struct {
			Message string `json:"message"`
		}{sc.Text})
		if err != nil {
			return err
		}
		return w.processPayload(ctx, &outgress.Message{
			Type:          outgress.TypeAnnounce,
			BroadcasterID: broadcasterID,
			Color:         sc.Color,
			Payload:       body,
		})
	default: // TypePin: the pin action sends the message first, then pins it.
		if sc.Text == "" {
			return nil
		}
		body, err := sonic.Marshal(struct {
			BroadcasterID string `json:"broadcaster_id"`
			Message       string `json:"message"`
		}{broadcasterID, sc.Text})
		if err != nil {
			return err
		}
		return w.processPayload(ctx, &outgress.Message{
			Type:          outgress.TypePin,
			BroadcasterID: broadcasterID,
			Payload:       body,
		})
	}
}

// sendBotChat routes one synthetic bot chat line (a clip reply, the reauth
// beacon) through the ordinary chat action — registry route defaults, bot
// sender injection, per-channel chat rate bucket — exactly as if a lane job
// carried it.
func (w *Worker) sendBotChat(ctx context.Context, broadcasterID, text string) error {
	body, err := sonic.Marshal(struct {
		BroadcasterID string `json:"broadcaster_id"`
		Message       string `json:"message"`
	}{broadcasterID, text})
	if err != nil {
		return err
	}
	chat := outgress.Message{
		Type:          outgress.TypeChat,
		BroadcasterID: broadcasterID,
		Payload:       body,
	}
	return w.processPayload(ctx, &chat)
}

func (w *Worker) decodePayload(ctx context.Context, data []byte, payload *outgress.Message) bool {
	decodeStarted := time.Now()
	err := decodeMessage(data, payload)
	recordStageDuration(ctx, "outgress.decode_ms", decodeStarted)
	if err != nil {
		w.log.Error("dropping malformed outgress message", zap.Error(err))
		noticeError(ctx, err)
		return false
	}
	return true
}

func annotateTxn(ctx context.Context, payload *outgress.Message) {
	txn := newrelic.FromContext(ctx)
	if txn == nil {
		return
	}
	txn.AddAttribute("node.region", nodeRegion)
	txn.AddAttribute("node.name", nodeName)
	txn.AddAttribute("event.type", payload.Type)
	txn.AddAttribute("event.broadcaster_id", payload.BroadcasterID)
	if payload.Endpoint != "" {
		txn.AddAttribute("event.endpoint", payload.Endpoint)
	}
}

func (w *Worker) checkPaused(ctx context.Context) error {
	pauseStarted := time.Now()
	paused, err := w.registry.Paused(ctx)
	recordStageDuration(ctx, "outgress.pause_ms", pauseStarted)
	if err != nil {
		return err
	}
	if paused {
		return ErrPaused
	}
	return nil
}
