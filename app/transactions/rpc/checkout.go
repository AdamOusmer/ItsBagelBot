package rpc

import (
	"context"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/transactions/tebex"
	transactionsrpc "ItsBagelBot/internal/domain/rpc/transactions"
	"ItsBagelBot/pkg/bus"
)

type checkoutRPC struct {
	tebex *tebex.Client
	log   *zap.Logger
}

// SubscribeCheckout registers the dashboard-facing basket_create verb: mint a
// Tebex basket for one user so the dashboard can launch the embedded checkout.
func SubscribeCheckout(nc *nats.Conn, client *tebex.Client, prefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	c := &checkoutRPC{tebex: client, log: log}

	// Basket creation is two upstream HTTP calls, so give it more room than the
	// default in-cluster RPC budget.
	return bus.QueueSubscribeJSON[transactionsrpc.BasketCreateRequest, transactionsrpc.BasketCreateReply](
		nc, prefix+".basket_create", queueGroup, 15*time.Second, app, log, c.basketCreate)
}

func (c *checkoutRPC) basketCreate(ctx context.Context, req transactionsrpc.BasketCreateRequest) transactionsrpc.BasketCreateReply {

	userID, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil || userID == 0 {
		return transactionsrpc.BasketCreateReply{Error: "user_id must be numeric"}
	}

	basket, err := c.tebex.CreateBasket(ctx, userID, req.Username)
	if err != nil {
		c.log.Warn("tebex basket create failed", zap.Uint64("user_id", userID), zap.Error(err))
		return transactionsrpc.BasketCreateReply{Error: "checkout is unavailable right now"}
	}

	return transactionsrpc.BasketCreateReply{
		Ident:       basket.Ident,
		CheckoutURL: basket.CheckoutURL,
	}
}
