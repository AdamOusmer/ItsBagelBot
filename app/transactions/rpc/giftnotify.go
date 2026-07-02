package rpc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/app/transactions/web"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	"ItsBagelBot/pkg/bus"
)

// GiftNotifier sends the "you received premium" direct notification through the
// notifications service admin send RPC when a gifted payment lands.
type GiftNotifier struct {
	nc          *nats.Conn
	sendSubject string
}

func NewGiftNotifier(nc *nats.Conn, sendSubject string) *GiftNotifier {
	return &GiftNotifier{nc: nc, sendSubject: sendSubject}
}

// Notify satisfies web.Config.NotifyGift. The webhook id doubles as the
// notification request id, so Tebex webhook retries collapse into one row on
// the notifications side.
func (g *GiftNotifier) Notify(ctx context.Context, notice web.GiftNotice) error {

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
