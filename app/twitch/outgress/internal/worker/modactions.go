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
	name   string
	method string
	path   string
}

var (
	banAction    = modAction{"ban/timeout", http.MethodPost, "/helix/moderation/bans"}
	shieldAction = modAction{"shield_mode", http.MethodPut, "/helix/moderation/shield_mode"}
	warnAction   = modAction{"warn", http.MethodPost, "/helix/moderation/warnings"}
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

	return w.execute(ctx, payload)
}

func (w *Worker) processBan(ctx context.Context, payload *outgress.Message) error {
	err := w.processModAction(ctx, payload, banAction)
	if err == nil {
		activity.Emit(ctx, payload.BroadcasterID, activity.Row{
			Kind: activity.KindAutomod,
			Text: i18n.T(payload.Locale, "activity.automod.ban_timeout"),
			At:   time.Now(),
		})
	}
	return err
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

func (w *Worker) processDelete(ctx context.Context, payload *outgress.Message) error {
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

	err := w.execute(ctx, payload)
	if err == nil {
		activity.Emit(ctx, payload.BroadcasterID, activity.Row{
			Kind: activity.KindAutomod,
			Text: i18n.T(payload.Locale, "activity.automod.deleted"),
			At:   time.Now(),
		})
	}
	return err
}

func deleteEndpoint(broadcasterID, moderatorID, msgID string) string {
	return "/helix/moderation/chat?broadcaster_id=" + url.QueryEscape(broadcasterID) +
		"&moderator_id=" + url.QueryEscape(moderatorID) +
		"&message_id=" + url.QueryEscape(msgID)
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
