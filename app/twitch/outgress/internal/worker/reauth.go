// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/i18n"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"

	"go.uber.org/zap"
)

type notice struct {
	title   string
	body    string
	chat    string
	request string
}

var (
	noticeRevoked = notice{
		title:   i18n.KeyReauthRevokedTitle,
		body:    i18n.KeyReauthRevokedBody,
		chat:    i18n.KeyReauthRevokedChat,
		request: "authz-revoked-",
	}
	noticeGrantDead = notice{
		title:   i18n.KeyGrantDeadTitle,
		body:    i18n.KeyGrantDeadBody,
		chat:    i18n.KeyGrantDeadChat,
		request: "grant-dead-",
	}
	noticeBanned = notice{
		title:   i18n.KeyBotBannedTitle,
		body:    i18n.KeyBotBannedBody,
		request: "bot-banned-",
	}
)

type ReauthConfig struct {
	SendSubject   string
	StateSubject  string
	ActiveSubject string
	BotID         string
}

func (r *ReauthNotifier) SetActive(ctx context.Context, broadcasterID string, active bool) error {
	_, err := bus.RequestJSONTimeout[map[string]any](ctx, r.nc, r.cfg.ActiveSubject,
		usersrpc.ActiveSetRequest{BroadcasterUserID: broadcasterID, Active: active}, 3*time.Second)
	return err
}

type ReauthNotifier struct {
	nc  *nats.Conn
	cfg ReauthConfig
	log *zap.Logger
}

func NewReauthNotifier(nc *nats.Conn, cfg ReauthConfig, log *zap.Logger) *ReauthNotifier {
	return &ReauthNotifier{nc: nc, cfg: cfg, log: log}
}

func (r *ReauthNotifier) Notify(ctx context.Context, broadcasterID string, n notice) {
	r.NotifyLocalized(ctx, broadcasterID, r.ResolveLocale(ctx, broadcasterID), n)
}

func (r *ReauthNotifier) NotifyLocalized(ctx context.Context, broadcasterID, locale string, n notice) {
	if _, err := strconv.ParseUint(r.cfg.BotID, 10, 64); err != nil {
		r.log.Warn("reauth notification skipped: bot id not numeric, no actor to send as",
			zap.String("broadcaster_id", broadcasterID))
		return
	}

	day := time.Now().UTC().Format("2006-01-02")

	reply, err := bus.RequestJSONTimeout[notificationsrpc.SendReply](ctx, r.nc, r.cfg.SendSubject,
		notificationsrpc.SendRequest{
			Scope:        "direct",
			TargetUserID: broadcasterID,
			Title:        i18n.T(locale, n.title),
			Body:         i18n.T(locale, n.body),
			Level:        "warning",
			ActorID:      r.cfg.BotID,
			ActorLogin:   "system",
			RequestID:    n.request + broadcasterID + "-" + day,
		}, 5*time.Second)

	r.logSendResult(broadcasterID, reply, err)
}

func (r *ReauthNotifier) logSendResult(broadcasterID string, reply notificationsrpc.SendReply, err error) {
	switch {
	case err != nil:
		r.log.Warn("reauth notification send failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	case reply.Error != "":
		r.log.Warn("reauth notification rejected",
			zap.String("broadcaster_id", broadcasterID), zap.String("error", reply.Error))
	default:
		r.log.Info("reauth notification sent", zap.String("broadcaster_id", broadcasterID))
	}
}

func (r *ReauthNotifier) ChatLine(locale string, n notice) string {
	return i18n.T(locale, n.chat)
}

func (r *ReauthNotifier) ResolveLocale(ctx context.Context, broadcasterID string) string {
	reply, err := bus.RequestJSONTimeout[usersrpc.StateGetReply](ctx, r.nc, r.cfg.StateSubject,
		usersrpc.StateGetRequest{BroadcasterUserID: broadcasterID}, 3*time.Second)
	if err != nil {
		r.log.Debug("locale lookup failed, defaulting to en",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return i18n.DefaultLocale
	}
	if reply.Locale == "" {
		return i18n.DefaultLocale
	}
	return reply.Locale
}
