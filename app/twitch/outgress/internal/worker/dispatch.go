// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/outgress"
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
	if w.rejectTrialOutput(ctx, &payload) {
		return nil
	}

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

func (w *Worker) processPayload(ctx context.Context, payload *outgress.Message) error {
	if w.rejectTrialOutput(ctx, payload) {
		return nil
	}
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

// Origin tag only: a trial-membership lookup here would stall every send on a Valkey error.
func (w *Worker) rejectTrialOutput(ctx context.Context, payload *outgress.Message) bool {
	if payload.Origin != "trial" {
		return false
	}
	w.countTrialBlocked(ctx, payload.BroadcasterID)
	return true
}

func (w *Worker) countTrialBlocked(ctx context.Context, id string) {
	if w.trialStore == nil || id == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	_ = w.trialStore.Do(ctx, w.trialStore.B().Hincrby().Key("trial:channel:"+id).Field("blocked").Increment(1).Build()).Error()
}

func (w *Worker) sendBotLine(ctx context.Context, broadcasterID, text string) error {
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

func slashMessage(broadcasterID string, sc outgress.SlashCommand) (*outgress.Message, error) {
	if sc.Type == outgress.TypeShoutout {
		return shoutoutMessage(broadcasterID, sc), nil
	}
	return textActionMessage(broadcasterID, sc)
}

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

func (w *Worker) sendBotChat(ctx context.Context, broadcasterID, text string) error {
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
