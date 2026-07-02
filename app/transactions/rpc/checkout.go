package rpc

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/transactions/tebex"
	transactionsrpc "ItsBagelBot/internal/domain/rpc/transactions"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

type checkoutRPC struct {
	tebex          *tebex.Client
	nc             *nats.Conn
	userGetSubject string
	log            *zap.Logger
}

// SubscribeCheckout registers the dashboard-facing basket_create verb: mint a
// Tebex basket so the dashboard can redirect to Tebex-hosted checkout, either
// for the signed-in buyer or as a gift to another registered user.
// userGetSubject is the users service admin lookup (bagel.rpc.admin.user.get)
// used to resolve and vet gift recipients.
func SubscribeCheckout(nc *nats.Conn, client *tebex.Client, prefix, userGetSubject, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	c := &checkoutRPC{tebex: client, nc: nc, userGetSubject: userGetSubject, log: log}

	// Basket creation is two upstream HTTP calls (plus a recipient lookup for
	// gifts), so give it more room than the default in-cluster RPC budget.
	return bus.QueueSubscribeJSON[transactionsrpc.BasketCreateRequest, transactionsrpc.BasketCreateReply](
		nc, prefix+".basket_create", queueGroup, 15*time.Second, app, log, c.basketCreate)
}

func (c *checkoutRPC) basketCreate(ctx context.Context, req transactionsrpc.BasketCreateRequest) transactionsrpc.BasketCreateReply {

	buyerID, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil || buyerID == 0 {
		return transactionsrpc.BasketCreateReply{Error: "user_id must be numeric"}
	}

	packageType := ""
	switch req.PackageType {
	case "", "single", "subscription":
		packageType = req.PackageType
	default:
		return transactionsrpc.BasketCreateReply{Error: "package_type must be single or subscription"}
	}

	spec := tebex.BasketSpec{UserID: buyerID, Username: req.Username, IPAddress: validIPv4(req.IPAddress), PackageType: packageType}
	recipientLogin := ""

	if recipient := normalizeLogin(req.RecipientUsername); recipient != "" {
		view, err := c.resolveRecipient(ctx, recipient)
		if err != nil {
			return transactionsrpc.BasketCreateReply{Error: err.Error()}
		}
		if view.ID == buyerID {
			return transactionsrpc.BasketCreateReply{Error: "that is your own account — use Subscribe instead"}
		}
		spec = tebex.BasketSpec{
			UserID:        view.ID,
			Username:      view.Username,
			IPAddress:     validIPv4(req.IPAddress),
			GiftedByID:    buyerID,
			GiftedByLogin: req.Username,
			// A gift is one paid month, never a recurring charge on the buyer.
			PackageType: "single",
		}
		recipientLogin = view.Username
	}

	basket, err := c.tebex.CreateBasket(ctx, spec)
	if err != nil {
		c.log.Warn("tebex basket create failed",
			zap.Uint64("user_id", spec.UserID), zap.Uint64("gifted_by", spec.GiftedByID), zap.Error(err))
		return transactionsrpc.BasketCreateReply{Error: "checkout is unavailable right now"}
	}

	return transactionsrpc.BasketCreateReply{
		Ident:          basket.Ident,
		CheckoutURL:    basket.CheckoutURL,
		RecipientLogin: recipientLogin,
	}
}

func validIPv4(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil || ip.To4() == nil {
		return ""
	}
	return ip.String()
}

// resolveRecipient vets a gift target: the Twitch login must belong to a
// registered ItsBagelBot user who is not banned and does not already have a
// paid or VIP plan. Error strings are user-facing (the dashboard surfaces them
// on the gift form verbatim).
func (c *checkoutRPC) resolveRecipient(ctx context.Context, login string) (*usersrpc.AdminUserView, error) {

	reply, err := bus.RequestJSONTimeout[usersrpc.AdminReply](ctx, c.nc, c.userGetSubject,
		usersrpc.AdminRequest{Username: login}, 3*time.Second)
	if err != nil {
		if _, isReply := err.(bus.RPCReplyError); isReply {
			return nil, errRecipientNotRegistered
		}
		c.log.Warn("gift recipient lookup failed", zap.String("login", login), zap.Error(err))
		return nil, errRecipientLookup
	}
	if reply.User == nil {
		return nil, errRecipientNotRegistered
	}
	if reply.User.Banned {
		return nil, errRecipientNotEligible
	}
	switch strings.ToLower(reply.User.Status) {
	case "paid", "vip":
		return nil, errRecipientAlreadyPremium
	}
	return reply.User, nil
}

var (
	errRecipientNotRegistered  = constError("that user hasn't signed in to ItsBagelBot yet, so premium can't be gifted to them")
	errRecipientNotEligible    = constError("that account can't receive premium")
	errRecipientAlreadyPremium = constError("that user already has premium")
	errRecipientLookup         = constError("could not verify the recipient right now — try again in a moment")
)

type constError string

func (e constError) Error() string { return string(e) }

// normalizeLogin turns user input into a Twitch login: trimmed, lowercased,
// leading @ dropped.
func normalizeLogin(input string) string {
	login := strings.TrimSpace(input)
	login = strings.TrimPrefix(login, "@")
	return strings.ToLower(login)
}
