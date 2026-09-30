// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/rpc"
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

// noticeEpisode ties a notice to the moment its underlying block or grant-dead
// state began, so repeat sends for the same unresolved episode share one
// RequestID and dedup at the notifications table instead of paging the
// broadcaster once a day (or once a beacon cycle) until they act.
type noticeEpisode struct {
	notice
	episode time.Time
}

// Notify resolves the locale and sends, unless the broadcaster has no
// account to notify (e.g. a trial channel shadow-read without a users row).
func (r *ReauthNotifier) Notify(ctx context.Context, broadcasterID string, n notice, episode time.Time) {
	r.notifyKnown(ctx, broadcasterID, n, episode)
}

// notifyKnown returns the resolved locale even when the send is skipped, so
// callers that also owe the broadcaster a chat line can still localize it.
func (r *ReauthNotifier) notifyKnown(ctx context.Context, broadcasterID string, n notice, episode time.Time) string {
	locale, found := r.ResolveLocale(ctx, broadcasterID)
	if !found {
		r.log.Info("reauth notification skipped: broadcaster has no account",
			zap.String("broadcaster_id", broadcasterID))
		return locale
	}
	r.NotifyLocalized(ctx, broadcasterID, locale, noticeEpisode{notice: n, episode: episode})
	return locale
}

func (r *ReauthNotifier) NotifyLocalized(ctx context.Context, broadcasterID, locale string, ne noticeEpisode) {
	if _, err := strconv.ParseUint(r.cfg.BotID, 10, 64); err != nil {
		r.log.Warn("reauth notification skipped: bot id not numeric, no actor to send as",
			zap.String("broadcaster_id", broadcasterID))
		return
	}

	reply, err := bus.RequestJSONTimeout[notificationsrpc.SendReply](ctx, r.nc, r.cfg.SendSubject,
		notificationsrpc.SendRequest{
			Scope:        "direct",
			TargetUserID: broadcasterID,
			Title:        i18n.T(locale, ne.title),
			Body:         i18n.T(locale, ne.body),
			Level:        "warning",
			ActorID:      r.cfg.BotID,
			ActorLogin:   "system",
			RequestID:    ne.request + broadcasterID + "-" + episodeStamp(ne.episode),
		}, 5*time.Second)

	r.logSendResult(broadcasterID, reply, err)
}

// episodeStamp falls back to the day when no episode start is known, keeping
// the old daily-dedup behavior rather than sending an unkeyed notification.
func episodeStamp(episode time.Time) string {
	if episode.IsZero() {
		return time.Now().UTC().Format("2006-01-02")
	}
	return strconv.FormatInt(episode.Unix(), 10)
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

// ResolveLocale reports the broadcaster's locale and whether they have an
// account at all. A transport failure resolves to (default locale, found)
// rather than (default locale, not found): "found" must mean the users
// lookup positively came back not_found, not that we couldn't ask.
func (r *ReauthNotifier) ResolveLocale(ctx context.Context, broadcasterID string) (string, bool) {
	reply, err := bus.RequestJSONTimeout[usersrpc.StateGetReply](ctx, r.nc, r.cfg.StateSubject,
		usersrpc.StateGetRequest{BroadcasterUserID: broadcasterID}, 3*time.Second)
	if err != nil {
		r.log.Debug("locale lookup failed, defaulting to en",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return i18n.DefaultLocale, true
	}
	if reply.Code == rpc.CodeNotFound {
		return i18n.DefaultLocale, false
	}
	if reply.Locale == "" {
		return i18n.DefaultLocale, true
	}
	return reply.Locale, true
}
