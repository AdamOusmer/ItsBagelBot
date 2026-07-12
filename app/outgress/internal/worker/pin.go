package worker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// processPin sends the command response as a normal bot chat message, reads the
// message id Twitch assigns it, then pins that id. duration_seconds is
// intentionally absent from pinEndpoint: Twitch therefore keeps the pin for the
// remainder of the current stream instead of applying a 30-1800 second timer.
func (w *Worker) processPin(ctx context.Context, payload outgress.Message) error {
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
	w.finishPin(ctx, pin, messageID)
	return nil
}

// preparePin reserves both rate-limit paths before the chat send. Once the
// message exists, retrying the whole queue item would duplicate it just to retry
// the pin, so every locally predictable wait must happen first.
func (w *Worker) preparePin(ctx context.Context, payload outgress.Message) (outgress.Message, string, bool, error) {
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
	if err := w.takeGeneralHelix(ctx, pin); err != nil {
		return outgress.Message{}, "", false, err
	}
	if err := w.takePinChat(ctx, payload); err != nil {
		return outgress.Message{}, "", false, err
	}
	return pin, mod, true, nil
}

func (w *Worker) takePinChat(ctx context.Context, payload outgress.Message) error {
	registryStarted := time.Now()
	ch, found, err := w.registry.Get(ctx, payload.BroadcasterID)
	recordStageDuration(ctx, "outgress.registry_ms", registryStarted)
	if err != nil {
		return err
	}
	return w.takeChat(ctx, payload.BroadcasterID, w.modStatus(ctx, payload, ch, found))
}

// sendPinChat posts the text through the ordinary chat route and returns the id
// Twitch assigned. An empty id means Twitch rejected or dropped the send and the
// pin action is safely consumed.
func (w *Worker) sendPinChat(ctx context.Context, payload outgress.Message, mod string) (string, error) {
	chatRoute := typeRoutes[outgress.TypeChat]
	chat := payload
	chat.Type = outgress.TypeChat
	chat.Endpoint = chatRoute.endpoint
	chat.Method = chatRoute.method
	chat.As = chatRoute.as
	chat.Payload = withSenderID(chat.Payload, mod)

	res, err := w.executeRequest(ctx, chat)
	if err != nil {
		return "", err
	}
	defer drainResponse(res)
	return w.pinMessageID(ctx, payload, chat, res)
}

func (w *Worker) pinMessageID(ctx context.Context, payload, chat outgress.Message, res *http.Response) (string, error) {
	status := res.StatusCode
	if err := w.helixResult(ctx, chat, res); err != nil {
		return "", err
	}
	if status >= http.StatusBadRequest {
		return "", nil
	}

	messageID, sent, decodeErr := sentChatMessageID(res.Body)
	if decodeErr != nil {
		// Twitch accepted the send, so redelivery would duplicate it. Surface the
		// bad response and ack this item instead of creating chat spam.
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

func (w *Worker) finishPin(ctx context.Context, pin outgress.Message, messageID string) {
	if err := w.execute(ctx, pin); err != nil {
		// The chat message already exists. Retrying this compound action would send
		// it again, so report the pin failure but consume the queue item.
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

// sentChatMessageID reads the Send Chat Message response. A successful Helix
// response may still carry is_sent=false (for example, chat rejected the line),
// so callers must check both return values before pinning.
func sentChatMessageID(r io.Reader) (messageID string, sent bool, err error) {
	var reply sendChatReply
	if err := json.NewDecoder(io.LimitReader(r, 4096)).Decode(&reply); err != nil {
		return "", false, err
	}
	if len(reply.Data) == 0 {
		return "", false, nil
	}
	return reply.Data[0].MessageID, reply.Data[0].IsSent, nil
}

// pinEndpoint deliberately has no duration_seconds. Per Twitch's contract,
// omission means the message remains pinned until the stream ends.
func pinEndpoint(broadcasterID, moderatorID, messageID string) string {
	return "/helix/chat/pins?broadcaster_id=" + url.QueryEscape(broadcasterID) +
		"&moderator_id=" + url.QueryEscape(moderatorID) +
		"&message_id=" + url.QueryEscape(messageID)
}
