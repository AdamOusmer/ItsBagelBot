package worker

import (
	"context"
	"strconv"
	"time"

	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"

	"go.uber.org/zap"
)

// dashboardURL is where a streamer re-consents; baked into the reauth copy.
const dashboardURL = "https://dashboard.itsbagelbot.com"

// reauthCopy is one locale's reauth wording: the dashboard bell notification
// (title + body) and the go-live chat beacon line.
type reauthCopy struct {
	title string
	body  string
	chat  string
}

// reauthMessages carries every supported locale, keyed by the users service's
// locale codes. English is the fallback for unknown codes, mirroring the
// console's translate() fallback.
var reauthMessages = map[string]reauthCopy{
	"en": {
		title: "Bot lost access to your channel",
		body: "Twitch revoked the bot's authorization for your channel (this happens after a password change or if the app was disconnected). " +
			"Chat replies and alerts are paused. Log in at " + dashboardURL + " and reconnect your Twitch account to bring the bot back.",
		chat: "The bot lost its Twitch authorization for this channel (password change or app disconnect). " +
			"Log in at " + dashboardURL + " to reconnect it.",
	},
	"fr": {
		title: "Le bot a perdu l'accès à votre chaîne",
		body: "Twitch a révoqué l'autorisation du bot pour votre chaîne (cela arrive après un changement de mot de passe ou si l'application a été déconnectée). " +
			"Les réponses et alertes sont en pause. Connectez-vous sur " + dashboardURL + " et reconnectez votre compte Twitch pour rétablir le bot.",
		chat: "Le bot a perdu son autorisation Twitch pour cette chaîne (changement de mot de passe ou déconnexion de l'application). " +
			"Connectez-vous sur " + dashboardURL + " pour le reconnecter.",
	},
}

// ReauthConfig carries the NATS wiring for the reauth messaging: the
// notifications admin send verb, the users state_get verb the locale comes
// from, and the bot's numeric user id (the actor the notifications service
// requires).
type ReauthConfig struct {
	SendSubject  string
	StateSubject string
	BotID        string
}

// ReauthNotifier tells a streamer their consent died and how to fix it, in
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

// NotifyRevoked sends the dashboard bell notification. Best-effort: the
// revoked registry state is already persisted by the caller and the console
// surfaces it on its own; a lost notification loses only the nudge. The
// request id folds the repeated per-subscription revocations of one outage
// into a single row per day.
func (r *ReauthNotifier) NotifyRevoked(ctx context.Context, broadcasterID string) {
	if _, err := strconv.ParseUint(r.cfg.BotID, 10, 64); err != nil {
		r.log.Warn("reauth notification skipped: bot id not numeric, no actor to send as",
			zap.String("broadcaster_id", broadcasterID))
		return
	}

	copyFor := r.localizedCopy(ctx, broadcasterID)
	day := time.Now().UTC().Format("2006-01-02")

	reply, err := bus.RequestJSONTimeout[notificationsrpc.SendReply](ctx, r.nc, r.cfg.SendSubject,
		notificationsrpc.SendRequest{
			Scope:        "direct",
			TargetUserID: broadcasterID,
			Title:        copyFor.title,
			Body:         copyFor.body,
			Level:        "warning",
			ActorID:      r.cfg.BotID,
			ActorLogin:   "system",
			RequestID:    "authz-revoked-" + broadcasterID + "-" + day,
		}, 5*time.Second)

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

// ChatBeacon returns the localized go-live chat line asking the streamer to
// reconnect.
func (r *ReauthNotifier) ChatBeacon(ctx context.Context, broadcasterID string) string {
	return r.localizedCopy(ctx, broadcasterID).chat
}

// localizedCopy resolves the broadcaster's dashboard locale, falling back to
// English on any failure or unknown code.
func (r *ReauthNotifier) localizedCopy(ctx context.Context, broadcasterID string) reauthCopy {
	locale := "en"

	reply, err := bus.RequestJSONTimeout[usersrpc.StateGetReply](ctx, r.nc, r.cfg.StateSubject,
		usersrpc.StateGetRequest{BroadcasterUserID: broadcasterID}, 3*time.Second)
	if err != nil {
		r.log.Debug("locale lookup failed, defaulting to en",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	} else if reply.Locale != "" {
		locale = reply.Locale
	}

	if c, ok := reauthMessages[locale]; ok {
		return c
	}
	return reauthMessages["en"]
}
