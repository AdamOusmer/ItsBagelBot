package worker

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// modAction names one Helix moderator action: the log label plus the method
// and path of the call. broadcaster_id and moderator_id ride the query string
// (Twitch reads them there, not the body); the body, when any, is built by the
// producer.
//
//   - ban/timeout: the body carries {data:{user_id,duration,reason}}, where the
//     presence of a duration makes it a timeout rather than a permanent ban.
//   - shield_mode: the body carries {"is_active":bool}. It is a single
//     channel-level call the automod escalates to instead of banning a whole
//     mass-raid account by account, so one PUT replaces thousands of bans.
//   - warn: the chatter must acknowledge the warning before chatting again;
//     the body carries {"data":{"user_id","reason"}}.
type modAction struct {
	name   string
	method string
	path   string
}

var (
	banAction    = modAction{"ban/timeout", http.MethodPost, "/helix/moderation/bans"}
	shieldAction = modAction{"shield_mode", http.MethodPut, "/helix/moderation/shield_mode"}
	warnAction   = modAction{"warn", http.MethodPost, "/helix/moderation/warnings"}
)

// processModAction issues one moderator action as the bot. It pays the general
// Helix budget, then hands the assembled request to execute() for the shared
// status handling.
func (w *Worker) processModAction(ctx context.Context, payload outgress.Message, action modAction) error {
	mod, ok := w.botIdentity(action.name, payload)
	if !ok {
		return nil
	}

	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}

	payload.As = outgress.AsBot
	payload.Method = action.method
	payload.Endpoint = modEndpoint(action.path, payload.BroadcasterID, mod)

	return w.execute(ctx, payload)
}

func (w *Worker) processBan(ctx context.Context, payload outgress.Message) error {
	return w.processModAction(ctx, payload, banAction)
}

func (w *Worker) processShieldMode(ctx context.Context, payload outgress.Message) error {
	return w.processModAction(ctx, payload, shieldAction)
}

func (w *Worker) processWarn(ctx context.Context, payload outgress.Message) error {
	return w.processModAction(ctx, payload, warnAction)
}

// modEndpoint assembles a moderator-action path: broadcaster_id + moderator_id
// ride the query string, URL-escaped.
func modEndpoint(path, broadcasterID, moderatorID string) string {
	return path + "?broadcaster_id=" + url.QueryEscape(broadcasterID) +
		"&moderator_id=" + url.QueryEscape(moderatorID)
}

// processAnnounce sends a Helix chat announcement as the bot. The endpoint
// carries broadcaster_id + moderator_id as query params, while the body
// carries the message plus a color (defaulting to "primary").
func (w *Worker) processAnnounce(ctx context.Context, payload outgress.Message) error {
	mod, ok := w.botIdentity("announce", payload)
	if !ok {
		return nil
	}

	// Announcements always execute on the app token; normalize before paying
	// the rate bucket so accounting matches the token the call runs under.
	payload.As = outgress.AsApp
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}

	color := payload.Color
	if color == "" {
		color = "primary"
	}

	payload.Method = http.MethodPost
	payload.Endpoint = modEndpoint("/helix/chat/announcements", payload.BroadcasterID, mod)
	payload.Payload = withField(payload.Payload, "color", color)

	return w.execute(ctx, payload)
}

// processDelete removes one chat message as the bot moderator (Helix Delete
// Chat Messages). Everything rides the query string: broadcaster_id +
// moderator_id + the target message_id (Message.MsgID); there is no body. A
// delete for an already-gone message is a 404 Twitch treats as permanent,
// which execute() drops - exactly right for a race with another bot or a
// human mod.
func (w *Worker) processDelete(ctx context.Context, payload outgress.Message) error {
	mod, ok := w.botIdentity("delete", payload)
	if !ok {
		return nil
	}
	if payload.MsgID == "" {
		w.log.Error("dropping delete: no message id",
			zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}

	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}

	payload.As = outgress.AsBot
	payload.Method = http.MethodDelete
	payload.Endpoint = deleteEndpoint(payload.BroadcasterID, mod, payload.MsgID)
	payload.Payload = nil

	return w.execute(ctx, payload)
}

// deleteEndpoint assembles the Helix Delete Chat Messages path; all three ids
// ride the query string, URL-escaped.
func deleteEndpoint(broadcasterID, moderatorID, msgID string) string {
	return "/helix/moderation/chat?broadcaster_id=" + url.QueryEscape(broadcasterID) +
		"&moderator_id=" + url.QueryEscape(moderatorID) +
		"&message_id=" + url.QueryEscape(msgID)
}

// processShoutout sends a Helix Send a Shoutout as the bot. The producer
// carries the source channel (BroadcasterID) plus the target login (To);
// outgress resolves the login to a numeric id (cached, single-flight) and owns
// the moderator identity. from/to/moderator ids ride the query string, never a
// body.
func (w *Worker) processShoutout(ctx context.Context, payload outgress.Message) error {
	if payload.To == "" {
		w.log.Error("dropping shoutout: no target login",
			zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}

	mod, ok := w.botIdentity("shoutout", payload)
	if !ok {
		return nil
	}

	toID, err := w.resolveShoutoutTarget(ctx, payload)
	if err != nil || toID == "" {
		return err
	}

	// Shoutouts always execute on the app token; normalize before paying the
	// rate bucket so accounting matches the token the call runs under.
	payload.As = outgress.AsApp
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}

	payload.Method = http.MethodPost
	payload.Endpoint = shoutoutEndpoint(payload.BroadcasterID, toID, mod)
	payload.Payload = nil

	return w.execute(ctx, payload)
}

// resolveShoutoutTarget resolves the target login to its numeric id (cached,
// single-flight). A loader error is transient (nack so paced redelivery
// retries); a "" id with nil error means no such user, which retrying can
// never fix, so the caller drops instead of nacking.
func (w *Worker) resolveShoutoutTarget(ctx context.Context, payload outgress.Message) (string, error) {
	toID, err := w.userIDs.GetOrLoad(ctx, "login:"+strings.ToLower(payload.To),
		func(ctx context.Context) (string, error) {
			return w.twitch.UserIDByLogin(ctx, payload.To)
		})
	if err != nil {
		w.log.Warn("shoutout target resolve failed, will retry",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("to", payload.To), zap.Error(err))
		return "", err
	}
	if toID == "" {
		w.log.Warn("dropping shoutout: no such target user",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("to", payload.To))
	}
	return toID, nil
}

// shoutoutEndpoint assembles the Helix Send a Shoutout path. All three ids ride
// the query string (Twitch reads them from the query, not a body) and are
// URL-escaped. Factored out so the construction can be pinned without a network
// round-trip.
func shoutoutEndpoint(fromBroadcasterID, toID, moderatorID string) string {
	return "/helix/chat/shoutouts?from_broadcaster_id=" + url.QueryEscape(fromBroadcasterID) +
		"&to_broadcaster_id=" + url.QueryEscape(toID) +
		"&moderator_id=" + url.QueryEscape(moderatorID)
}
