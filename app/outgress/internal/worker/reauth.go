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

// notice is one reason to nudge a streamer to reconnect: the catalog keys for
// each surface, plus the request-id prefix that scopes the bell's daily dedupe.
//
// The prefix must differ per reason. The notifications table's unique index on
// request_id carries no time component and the send verb skips cache
// invalidation on the dedupe path, so two reasons sharing a prefix would make
// the second one silently produce nothing for the rest of the UTC day.
type notice struct {
	title   string
	body    string
	chat    string
	request string
}

var (
	// noticeRevoked: Twitch told us over EventSub that the grant is gone.
	noticeRevoked = notice{
		title:   i18n.KeyReauthRevokedTitle,
		body:    i18n.KeyReauthRevokedBody,
		chat:    i18n.KeyReauthRevokedChat,
		request: "authz-revoked-",
	}
	// noticeGrantDead: the stored grant stopped working without ever being
	// revoked, so Twitch announced nothing and we only know because a call failed.
	noticeGrantDead = notice{
		title:   i18n.KeyGrantDeadTitle,
		body:    i18n.KeyGrantDeadBody,
		chat:    i18n.KeyGrantDeadChat,
		request: "grant-dead-",
	}
)

// ReauthConfig carries the NATS wiring for the reauth messaging: the
// notifications admin send verb, the users state_get verb the locale comes
// from, and the bot's numeric user id (the actor the notifications service
// requires).
type ReauthConfig struct {
	SendSubject  string
	StateSubject string
	BotID        string
}

// ReauthNotifier tells a streamer their grant died and how to fix it, in
// their dashboard language. It rides outgress's RPC connection: the locale
// comes from the users service (state_get) and the bell notification goes
// through the notifications admin send verb, the same one the gift
// notification uses.
type ReauthNotifier struct {
	nc  *nats.Conn
	cfg ReauthConfig
	log *zap.Logger
}

func NewReauthNotifier(nc *nats.Conn, cfg ReauthConfig, log *zap.Logger) *ReauthNotifier {
	return &ReauthNotifier{nc: nc, cfg: cfg, log: log}
}

// Notify resolves the locale and sends the dashboard bell.
func (r *ReauthNotifier) Notify(ctx context.Context, broadcasterID string, n notice) {
	r.NotifyLocalized(ctx, broadcasterID, r.ResolveLocale(ctx, broadcasterID), n)
}

// NotifyLocalized sends the dashboard bell with an already-resolved locale, so
// a caller that also posts the chat line pays one locale lookup rather than two.
//
// Best-effort: the registry state is already persisted by the caller and the
// console surfaces it independently, so a lost notification loses only the nudge.
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

// ChatLine returns the localized go-live chat line asking the streamer to
// reconnect.
func (r *ReauthNotifier) ChatLine(locale string, n notice) string {
	return i18n.T(locale, n.chat)
}

// ResolveLocale returns the broadcaster's dashboard locale, falling back to
// English on any failure or unknown code.
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
