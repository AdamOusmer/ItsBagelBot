// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"time"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

func (w *Worker) processPin(ctx context.Context, payload *outgress.Message) error {
	pin, mod, ok, err := w.preparePin(ctx, payload)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	messageID, err := w.sendPinChat(ctx, payload, mod)
	if err != nil {
		return err
	}
	if messageID == "" {
		return nil
	}

	pin.Endpoint = pinEndpoint(payload.BroadcasterID, mod, messageID)
	w.finishPin(ctx, &pin, messageID)
	return nil
}

func (w *Worker) preparePin(ctx context.Context, payload *outgress.Message) (outgress.Message, string, bool, error) {
	mod, ok := w.botIdentity("pin", payload)
	if !ok {
		return outgress.Message{}, "", false, nil
	}

	pin := outgress.Message{
		Type:          outgress.TypePin,
		BroadcasterID: payload.BroadcasterID,
		SenderID:      payload.SenderID,
		Endpoint:      "/helix/chat/pins",
		Method:        http.MethodPut,
		As:            outgress.AsApp,
	}
	if err := w.takeGeneralHelix(ctx, &pin); err != nil {
		return outgress.Message{}, "", false, err
	}
	if err := w.takePinChat(ctx, payload); err != nil {
		return outgress.Message{}, "", false, err
	}
	return pin, mod, true, nil
}

func (w *Worker) takePinChat(ctx context.Context, payload *outgress.Message) error {
	registryStarted := time.Now()
	ch, found := w.chatChannel(ctx, payload.BroadcasterID)
	recordStageDuration(ctx, "outgress.registry_ms", registryStarted)
	return w.takeChat(ctx, payload.BroadcasterID, w.modStatus(ctx, payload, ch, found))
}

func (w *Worker) sendPinChat(ctx context.Context, payload *outgress.Message, mod string) (string, error) {
	chatAction, _ := w.actions.Lookup(outgress.TypeChat)
	chat := *payload
	chat.Type = outgress.TypeChat
	chat.Endpoint = chatAction.Endpoint
	chat.Method = chatAction.Method
	chat.As = chatAction.As
	chat.Payload = withSenderID(chat.Payload, mod)

	res, err := w.executeRequest(ctx, &chat)
	if err != nil {
		return "", err
	}
	defer drainResponse(res)
	return w.pinMessageID(ctx, payload, &chat, res)
}

func (w *Worker) pinMessageID(ctx context.Context, payload, chat *outgress.Message, res *http.Response) (string, error) {
	status := res.StatusCode
	if err := w.helixResult(ctx, chat, res); err != nil {
		return "", err
	}
	if status >= http.StatusBadRequest {
		return "", nil
	}

	messageID, sent, decodeErr := sentChatMessageID(res.Body)
	if decodeErr != nil {
		w.log.Error("chat sent but pin response could not be decoded",
			zap.String("broadcaster_id", payload.BroadcasterID), zap.Error(decodeErr))
		noticeError(ctx, decodeErr)
		return "", nil
	}
	if !sent || messageID == "" {
		w.log.Warn("chat message was not sent; skipping pin",
			zap.String("broadcaster_id", payload.BroadcasterID))
		return "", nil
	}
	return messageID, nil
}

func (w *Worker) finishPin(ctx context.Context, pin *outgress.Message, messageID string) {
	if err := w.execute(ctx, pin); err != nil {
		w.log.Warn("chat sent but pin failed; skipping unsafe redelivery",
			zap.String("broadcaster_id", pin.BroadcasterID),
			zap.String("message_id", messageID), zap.Error(err))
		noticeError(ctx, err)
	}
}

type sendChatReply struct {
	Data []struct {
		MessageID string `json:"message_id"`
		IsSent    bool   `json:"is_sent"`
	} `json:"data"`
}

func sentChatMessageID(r io.Reader) (messageID string, sent bool, err error) {
	var reply sendChatReply
	if err := codec.NewDecoder(io.LimitReader(r, 4096)).Decode(&reply); err != nil {
		return "", false, err
	}
	if len(reply.Data) == 0 {
		return "", false, nil
	}
	return reply.Data[0].MessageID, reply.Data[0].IsSent, nil
}

func pinEndpoint(broadcasterID, moderatorID, messageID string) string {
	return "/helix/chat/pins?broadcaster_id=" + url.QueryEscape(broadcasterID) +
		"&moderator_id=" + url.QueryEscape(moderatorID) +
		"&message_id=" + url.QueryEscape(messageID)
}
