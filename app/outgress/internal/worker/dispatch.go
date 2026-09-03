// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"time"

	"ItsBagelBot/app/outgress/internal/action"
	"ItsBagelBot/app/outgress/internal/validate"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"

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
	// Trust boundary before any key is built or bucket paid: broadcaster ids
	// flow into dynamic Valkey keys and the registry index (see
	// validate.BroadcasterID for the injection shape this closes). Only routed
	// Twitch actions are checked — their ids must be numeric, while internal
	// jobs carry either no id or a foreign platform's id shape ("UC…",
	// Discord snowflakes aside) that digits-only would wrongly reject. An
	// empty id stays legal here: several system paths legitimately omit it.
	if act.Kind != action.KindInternal && payload.BroadcasterID != "" &&
		!validate.BroadcasterID(payload.BroadcasterID) {
		w.log.Error("dropping message with invalid broadcaster id",
			zap.String("type", payload.Type),
			zap.String("broadcaster_id", payload.BroadcasterID))
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
	if !w.botSpeechAllowed(ctx, broadcasterID, text) {
		return nil
	}
	sc, ok := outgress.CutSlash(text)
	if !ok {
		return w.sendBotChat(ctx, broadcasterID, text)
	}
	msg, err := slashMessage(broadcasterID, sc)
	if err != nil || msg == nil {
		return err
	}
	return w.processPayload(ctx, msg)
}

// slashMessage builds the outgress message for a routed slash action. It
// returns a nil message (and nil error) when the action carries no usable
// payload — an /announce or /pin with no text, a /shoutout with no target —
// so the caller drops it instead of sending a call Twitch would reject.
func slashMessage(broadcasterID string, sc outgress.SlashCommand) (*outgress.Message, error) {
	if sc.Type == outgress.TypeShoutout {
		return shoutoutMessage(broadcasterID, sc), nil
	}
	return textActionMessage(broadcasterID, sc)
}

// shoutoutMessage builds the Send a Shoutout job: the target rides a dedicated
// field, so the body is empty.
func shoutoutMessage(broadcasterID string, sc outgress.SlashCommand) *outgress.Message {
	if sc.To == "" {
		return nil
	}
	return &outgress.Message{
		Type:          outgress.TypeShoutout,
		BroadcasterID: broadcasterID,
		To:            sc.To,
		Payload:       []byte("{}"),
	}
}

// textActionMessage builds the /announce and /pin jobs, which share everything
// but their body. Color is empty for a pin (CutSlash sets it only on an
// announce), so it can be assigned unconditionally.
func textActionMessage(broadcasterID string, sc outgress.SlashCommand) (*outgress.Message, error) {
	if sc.Text == "" {
		return nil, nil
	}
	body, err := textActionBody(broadcasterID, sc)
	if err != nil {
		return nil, err
	}
	return &outgress.Message{
		Type:          sc.Type,
		BroadcasterID: broadcasterID,
		Color:         sc.Color,
		Payload:       body,
	}, nil
}

// textActionBody marshals the body Twitch wants: an announcement carries only
// the message, while a pin reuses the Send Chat Message body because the pin
// action posts the line first and then pins it for the current stream.
func textActionBody(broadcasterID string, sc outgress.SlashCommand) ([]byte, error) {
	if sc.Type == outgress.TypeAnnounce {
		return codec.Marshal(&struct {
			Message string `json:"message"`
		}{sc.Text})
	}
	return codec.Marshal(&struct {
		BroadcasterID string `json:"broadcaster_id"`
		Message       string `json:"message"`
	}{broadcasterID, sc.Text})
}

// sendBotChat routes one synthetic bot chat line (a clip reply, the reauth
// beacon) through the ordinary chat action — registry route defaults, bot
// sender injection, per-channel chat rate bucket — exactly as if a lane job
// carried it.
func (w *Worker) sendBotChat(ctx context.Context, broadcasterID, text string) error {
	if !w.botSpeechAllowed(ctx, broadcasterID, text) {
		return nil
	}
	body, err := codec.Marshal(&struct {
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

// botSpeechAllowed is the last-mile floor gate over the two composition
// funnels above: the only send paths where outgress itself authors the text
// (clip replies, reauth beacon, module outputs routed through here), splicing
// in viewer-controlled fragments such as clip titles and URLs. Everything
// upstream already filters its own output, but a compromised upstream or a
// crafted clip title must not turn the bot's own account into the delivery
// vehicle for an IP logger or worse, so both funnels re-check with the same
// immovable-floor predicate the save-time validators use (hate lexicon +
// IP-logger hosts; scam phrasing is deliberately excluded there because
// giveaway commands legitimately say "claim your prize"). A hit drops the
// line silently — it is a defense, not an error condition.
func (w *Worker) botSpeechAllowed(_ context.Context, _, text string) bool {
	if term, hit := moderation.CheckFloor(text); hit {
		w.log.Warn("dropping bot line: immovable floor content",
			zap.String("term", term))
		return false
	}
	return true
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
