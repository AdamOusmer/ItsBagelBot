// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/internal/activity"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

type modAction struct {
	name        string
	method      string
	path        string
	activityKey string
}

var (
	banAction    = modAction{"ban/timeout", http.MethodPost, "/helix/moderation/bans", "activity.automod.ban_timeout"}
	shieldAction = modAction{"shield_mode", http.MethodPut, "/helix/moderation/shield_mode", ""}
	warnAction   = modAction{"warn", http.MethodPost, "/helix/moderation/warnings", ""}
)

func (w *Worker) processModAction(ctx context.Context, payload *outgress.Message, action modAction) error {
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

	return w.executeModAction(ctx, payload, action.activityKey)
}

func (w *Worker) executeModAction(ctx context.Context, payload *outgress.Message, activityKey string) error {
	outcome, err := w.executeHelix(ctx, payload)
	if outcome == helixSucceeded && activityKey != "" {
		activity.Emit(ctx, payload.BroadcasterID, activity.Row{
			Kind: activity.KindAutomod,
			Text: i18n.T(payload.Locale, activityKey),
			At:   time.Now(),
		})
	}
	return err
}

func (w *Worker) processBan(ctx context.Context, payload *outgress.Message) error {
	return w.processModAction(ctx, payload, banAction)
}

func (w *Worker) processShieldMode(ctx context.Context, payload *outgress.Message) error {
	return w.processModAction(ctx, payload, shieldAction)
}

func (w *Worker) processWarn(ctx context.Context, payload *outgress.Message) error {
	return w.processModAction(ctx, payload, warnAction)
}

func modEndpoint(path, broadcasterID, moderatorID string) string {
	return path + "?broadcaster_id=" + url.QueryEscape(broadcasterID) +
		"&moderator_id=" + url.QueryEscape(moderatorID)
}

func (w *Worker) processAnnounce(ctx context.Context, payload *outgress.Message) error {
	mod, ok := w.botIdentity("announce", payload)
	if !ok {
		return nil
	}

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

type modDeleteAction struct {
	path        string
	queryKey    string
	missingID   string
	activityKey string
}

func (w *Worker) processDelete(ctx context.Context, payload *outgress.Message) error {
	return w.processModDelete(ctx, payload, payload.MsgID, modDeleteAction{
		path:        "/helix/moderation/chat",
		queryKey:    "message_id",
		missingID:   "dropping delete: no message id",
		activityKey: "activity.automod.deleted",
	})
}

func (w *Worker) processUnban(ctx context.Context, payload *outgress.Message) error {
	return w.processModDelete(ctx, payload, payload.TargetUserID, modDeleteAction{
		path:      "/helix/moderation/bans",
		queryKey:  "user_id",
		missingID: "dropping unban: no target user id",
	})
}

func (w *Worker) processModDelete(ctx context.Context, payload *outgress.Message, id string, action modDeleteAction) error {
	mod, ok := w.botIdentity(payload.Type, payload)
	if !ok {
		return nil
	}
	if id == "" {
		w.log.Error(action.missingID, zap.String("broadcaster_id", payload.BroadcasterID))
		return nil
	}

	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}

	payload.As = outgress.AsBot
	payload.Method = http.MethodDelete
	payload.Endpoint = modEndpoint(action.path, payload.BroadcasterID, mod) + "&" + action.queryKey + "=" + url.QueryEscape(id)
	payload.Payload = nil

	return w.executeModAction(ctx, payload, action.activityKey)
}

func (w *Worker) processShoutout(ctx context.Context, payload *outgress.Message) error {
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

	payload.As = outgress.AsApp
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}

	payload.Method = http.MethodPost
	payload.Endpoint = shoutoutEndpoint(payload.BroadcasterID, toID, mod)
	payload.Payload = nil

	return w.execute(ctx, payload)
}

func (w *Worker) resolveShoutoutTarget(ctx context.Context, payload *outgress.Message) (string, error) {
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

func shoutoutEndpoint(fromBroadcasterID, toID, moderatorID string) string {
	return "/helix/chat/shoutouts?from_broadcaster_id=" + url.QueryEscape(fromBroadcasterID) +
		"&to_broadcaster_id=" + url.QueryEscape(toID) +
		"&moderator_id=" + url.QueryEscape(moderatorID)
}
