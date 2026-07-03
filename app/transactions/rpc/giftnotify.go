package rpc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/transactions/mail"
	"ItsBagelBot/app/transactions/web"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

// GiftNotifier tells the recipient their gifted premium landed, on two
// independent channels: the in-app direct notification (notifications service
// admin send RPC) and, when a mailer is configured and the users service has
// a contact email on record, an email through Resend.
type GiftNotifier struct {
	nc           *nats.Conn
	sendSubject  string
	emailSubject string
	mailer       *mail.Mailer
	log          *zap.Logger
}

// NewGiftNotifier wires both channels. mailer may be nil: the email channel
// is then skipped entirely (RESEND_API_KEY not configured).
func NewGiftNotifier(nc *nats.Conn, sendSubject, emailSubject string, mailer *mail.Mailer, log *zap.Logger) *GiftNotifier {
	return &GiftNotifier{
		nc:           nc,
		sendSubject:  sendSubject,
		emailSubject: emailSubject,
		mailer:       mailer,
		log:          log,
	}
}

// Notify satisfies web.Config.NotifyGift. The webhook id doubles as the
// notification request id and the Resend idempotency key, so Tebex webhook
// retries collapse into one row and one email. The email leg is best-effort
// and self-logged: only the in-app failure propagates to the caller.
func (g *GiftNotifier) Notify(ctx context.Context, notice web.GiftNotice) error {

	g.sendEmail(ctx, notice)

	body := "You have been gifted ItsBagelBot Premium"
	if notice.GiftedByLogin != "" {
		body = fmt.Sprintf("%s gifted you ItsBagelBot Premium", notice.GiftedByLogin)
	}
	body += " — the priority lane and premium perks are active on your account now."

	reply, err := bus.RequestJSONTimeout[notificationsrpc.SendReply](ctx, g.nc, g.sendSubject,
		notificationsrpc.SendRequest{
			Scope:        "direct",
			TargetUserID: strconv.FormatUint(notice.RecipientID, 10),
			Title:        "You received Premium",
			Body:         body,
			Level:        "success",
			ActorID:      strconv.FormatUint(notice.GiftedByID, 10),
			ActorLogin:   notice.GiftedByLogin,
			RequestID:    "tebex-gift-" + notice.WebhookID,
		}, 5*time.Second)
	if err != nil {
		return err
	}
	if reply.Error != "" {
		return fmt.Errorf("notifications send: %s", reply.Error)
	}
	return nil
}

// sendEmail resolves the recipient's contact email through the users service
// and sends the gift email. Every outcome is logged without the address; a
// recipient with no email on record (never logged in since capture shipped)
// is a silent skip.
func (g *GiftNotifier) sendEmail(ctx context.Context, notice web.GiftNotice) {

	if g.mailer == nil {
		return
	}

	recipient := strconv.FormatUint(notice.RecipientID, 10)

	reply, err := bus.RequestJSONTimeout[usersrpc.EmailGetReply](ctx, g.nc, g.emailSubject,
		usersrpc.EmailGetRequest{UserID: recipient}, 3*time.Second)
	switch {
	case err != nil:
		g.log.Warn("gift email skipped: contact email lookup failed",
			zap.String("webhook_id", notice.WebhookID),
			zap.Uint64("recipient", notice.RecipientID),
			zap.Error(err))
		return
	case reply.Error != "":
		g.log.Warn("gift email skipped: contact email lookup failed",
			zap.String("webhook_id", notice.WebhookID),
			zap.Uint64("recipient", notice.RecipientID),
			zap.String("error", reply.Error))
		return
	case reply.Email == "":
		g.log.Info("gift email skipped: no contact email on record",
			zap.String("webhook_id", notice.WebhookID),
			zap.Uint64("recipient", notice.RecipientID))
		return
	}

	if err := g.mailer.SendGift(ctx, reply.Email, notice.GiftedByLogin, notice.GiftMessage, "tebex-gift-"+notice.WebhookID); err != nil {
		g.log.Warn("gift email send failed",
			zap.String("webhook_id", notice.WebhookID),
			zap.Uint64("recipient", notice.RecipientID),
			zap.Error(err))
		return
	}

	g.log.Info("gift email sent",
		zap.String("webhook_id", notice.WebhookID),
		zap.Uint64("recipient", notice.RecipientID))
}
