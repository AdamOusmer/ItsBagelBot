package worker

import (
	"context"
	"net/http"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

// helixRoute is the Helix call a message type maps to when the producer leaves
// endpoint/method empty. as is the default token identity for the type ("" =
// route by endpoint), applied only when the message does not set its own.
type helixRoute struct {
	method   string
	endpoint string
	as       string
}

// typeRoutes lets outgress own the Helix endpoint per type, so producers send
// intent ("chat", "ban", "ad", "clip") plus the body instead of hardcoding
// paths. Types absent here (e.g. "api") are generic passthroughs and must carry
// their own endpoint.
//
//   - chat:        bot send (app token honors user:bot + channel:bot).
//   - ban/unban:   bot acts as moderator (/helix/moderation/* → bot user token).
//   - timeout:     same endpoint as ban; the body's duration makes it a timeout.
//   - ad/commercial: the broadcaster starts the ad (channel:edit:commercial).
//   - clip:        the broadcaster's grant creates the clip (clips:edit).
var typeRoutes = map[string]helixRoute{
	outgress.TypeChat:       {http.MethodPost, "/helix/chat/messages", outgress.AsApp},
	outgress.TypeBan:        {http.MethodPost, "/helix/moderation/bans", outgress.AsBot},
	outgress.TypeTimeout:    {http.MethodPost, "/helix/moderation/bans", outgress.AsBot},
	outgress.TypeUnban:      {http.MethodDelete, "/helix/moderation/bans", outgress.AsBot},
	outgress.TypeAd:         {http.MethodPost, "/helix/channels/commercial", outgress.AsBroadcaster},
	outgress.TypeCommercial: {http.MethodPost, "/helix/channels/commercial", outgress.AsBroadcaster},
	outgress.TypeClip:       {http.MethodPost, "/helix/clips", outgress.AsBroadcaster},
	// Cloud-bot chat actions use the app token. Twitch only awards the Chat Bot
	// badge to Send Chat Message calls made with an app access token, backed by
	// the bot's user:bot/action grants and the broadcaster's channel:bot grant.
	outgress.TypeAnnounce: {http.MethodPost, "/helix/chat/announcements", outgress.AsApp},
	outgress.TypeShoutout: {http.MethodPost, "/helix/chat/shoutouts", outgress.AsApp},
	// Pin is a two-call action handled by processPin: send the chat message, then
	// PUT its returned message id here. Both calls use the cloud-bot app token.
	outgress.TypePin: {http.MethodPut, "/helix/chat/pins", outgress.AsApp},
	// Shield Mode is a moderator action (PUT /helix/moderation/shield_mode → bot
	// user token, moderator:manage:shield_mode). Like ban it needs broadcaster_id +
	// moderator_id on the query string, handled in processShieldMode.
	outgress.TypeShieldMode: {http.MethodPut, "/helix/moderation/shield_mode", outgress.AsBot},
	// Delete Chat Messages (moderator:manage:chat_messages) and Warn Chat User
	// (moderator:manage:warnings) are moderator actions too; query strings are
	// assembled in processDelete / processWarn.
	outgress.TypeDelete: {http.MethodDelete, "/helix/moderation/chat", outgress.AsBot},
	outgress.TypeWarn:   {http.MethodPost, "/helix/moderation/warnings", outgress.AsBot},
}

// helixHandlers routes the types that need their own request assembly (query
// string identities, response reads, chat rate buckets) instead of the generic
// helix passthrough. Every entry runs after resolveHelixRoute has filled the
// message's endpoint/method/as defaults.
var helixHandlers = map[string]func(*Worker, context.Context, outgress.Message) error{
	outgress.TypeChat:       (*Worker).processChat,
	outgress.TypeAnnounce:   (*Worker).processAnnounce,
	outgress.TypeShoutout:   (*Worker).processShoutout,
	outgress.TypePin:        (*Worker).processPin,
	outgress.TypeClip:       (*Worker).processClip,
	outgress.TypeBan:        (*Worker).processBan,
	outgress.TypeTimeout:    (*Worker).processBan,
	outgress.TypeShieldMode: (*Worker).processShieldMode,
	outgress.TypeDelete:     (*Worker).processDelete,
	outgress.TypeWarn:       (*Worker).processWarn,
}

func (w *Worker) Process(msg *bus.Message) error {
	ctx := msg.Context()
	processStarted := time.Now()
	defer recordStageDuration(ctx, "outgress.total_ms", processStarted)

	var payload outgress.Message
	if !w.decodePayload(ctx, msg.Payload, &payload) {
		return nil
	}
	annotateTxn(ctx, payload)

	if err := w.checkPaused(ctx); err != nil {
		return err
	}
	if payload.Type == outgress.TypeBatch {
		var batch outgress.Batch
		if err := decodeBatch(payload.Payload, &batch); err != nil {
			w.log.Error("dropping malformed outgress batch", zap.Error(err))
			noticeError(ctx, err)
			return nil
		}
		return w.processBatch(ctx, &batch, payload.BroadcasterID, msg.UUID)
	}
	return w.processPayload(ctx, payload)
}

// processPayload dispatches one decoded, admitted action. Batch jobs call it
// serially for each child; ordinary jobs call it once.
func (w *Worker) processPayload(ctx context.Context, payload outgress.Message) error {
	switch payload.Type {
	case outgress.TypeEventSub:
		return w.processEventSub(ctx, payload)
	case outgress.TypeStreamStatus:
		return w.processStreamStatus(ctx, payload)
	case outgress.TypeRedemptionUpdate:
		// Channel-points redemption resolution runs under the broadcaster token
		// and pays the general Helix budget, so it is handled before the
		// endpoint-routed path (it carries no endpoint of its own).
		return w.processRedemptionUpdate(ctx, payload)
	}

	if !w.resolveHelixRoute(&payload) {
		return nil
	}
	if handle, ok := helixHandlers[payload.Type]; ok {
		return handle(w, ctx, payload)
	}
	return w.processAPI(ctx, payload)
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

func annotateTxn(ctx context.Context, payload outgress.Message) {
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

// resolveHelixRoute fills endpoint/method/as from the message type when the
// producer left them empty, so a job only needs its intent + body. "api" has
// no mapping (generic passthrough) and must carry its own endpoint. An
// explicit field always wins, so any default can be overridden. It reports
// false (after logging) when the type is unknown or the resulting request is
// not a Helix call, and the message must be dropped.
func (w *Worker) resolveHelixRoute(payload *outgress.Message) bool {
	route, known := typeRoutes[payload.Type]
	if !known && payload.Type != outgress.TypeAPI {
		w.log.Error("dropping message with unknown type", zap.String("type", payload.Type))
		return false
	}
	if payload.Endpoint == "" {
		payload.Endpoint = route.endpoint
	}
	if payload.Method == "" {
		payload.Method = route.method
	}
	if payload.As == "" {
		payload.As = route.as
	}
	if !strings.HasPrefix(payload.Endpoint, "/helix/") || payload.Method == "" {
		w.log.Error("dropping message with invalid request",
			zap.String("type", payload.Type),
			zap.String("endpoint", payload.Endpoint),
			zap.String("method", payload.Method))
		return false
	}
	return true
}
