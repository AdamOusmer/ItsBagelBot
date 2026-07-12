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
	mod, ok := w.botIdentity("pin", payload)
	if !ok {
		return nil
	}

	// Reserve the general Helix allowance before posting the chat line. Once the
	// line exists, retrying the whole queue item would create a duplicate just to
	// retry the pin, so every locally predictable wait happens before the send.
	pin := outgress.Message{
		Type:          outgress.TypePin,
		BroadcasterID: payload.BroadcasterID,
		SenderID:      payload.SenderID,
		Endpoint:      "/helix/chat/pins",
		Method:        http.MethodPut,
		As:            outgress.AsApp,
	}
	if err := w.takeGeneralHelix(ctx, pin); err != nil {
		return err
	}

	registryStarted := time.Now()
	ch, found, err := w.registry.Get(ctx, payload.BroadcasterID)
	recordStageDuration(ctx, "outgress.registry_ms", registryStarted)
	if err != nil {
		return err
	}
	if err := w.takeChat(ctx, payload.BroadcasterID, w.modStatus(ctx, payload, ch, found)); err != nil {
		return err
	}

	chatRoute := typeRoutes[outgress.TypeChat]
	chat := payload
	chat.Type = outgress.TypeChat
	chat.Endpoint = chatRoute.endpoint
	chat.Method = chatRoute.method
	chat.As = chatRoute.as
	chat.Payload = withSenderID(chat.Payload, mod)

	res, err := w.executeRequest(ctx, chat)
	if err != nil {
		return err
	}
	status := res.StatusCode
	if err := w.helixResult(ctx, chat, res); err != nil {
		drainResponse(res)
		return err
	}
	if status >= http.StatusBadRequest {
		drainResponse(res)
		return nil
	}

	messageID, sent, decodeErr := sentChatMessageID(res.Body)
	drainResponse(res)
	if decodeErr != nil {
		// Twitch accepted the send, so redelivery would duplicate it. Surface the
		// bad response and ack this item instead of creating chat spam.
		w.log.Error("chat sent but pin response could not be decoded",
			zap.String("broadcaster_id", payload.BroadcasterID), zap.Error(decodeErr))
		noticeError(ctx, decodeErr)
		return nil
	}
	if !sent || messageID == "" {
		w.log.Warn("chat message was not sent; skipping pin",
			zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}

	pin.Endpoint = pinEndpoint(payload.BroadcasterID, mod, messageID)
	if err := w.execute(ctx, pin); err != nil {
		// The chat message already exists. Retrying this compound action would send
		// it again, so report the pin failure but consume the queue item.
		w.log.Warn("chat sent but pin failed; skipping unsafe redelivery",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("message_id", messageID), zap.Error(err))
		noticeError(ctx, err)
	}
	return nil
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
