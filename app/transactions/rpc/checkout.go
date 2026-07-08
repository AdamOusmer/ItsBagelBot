package rpc

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/transactions/tebex"
	transactionsrpc "ItsBagelBot/internal/domain/rpc/transactions"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
)

type checkoutRPC struct {
	tebex          *tebex.Client
	nc             *nats.Conn
	userGetSubject string
	log            *zap.Logger
}

// CheckoutConfig names the subjects the checkout RPC binds and resolves
// against. UserGetSubject is the users service admin lookup
// (bagel.rpc.admin.user.get) used to resolve and vet gift recipients.
type CheckoutConfig struct {
	Prefix         string
	UserGetSubject string
	QueueGroup     string
}

// CheckoutRuntime bundles the process-wide handles the checkout RPC binds
// against.
type CheckoutRuntime struct {
	NC  *nats.Conn
	App *newrelic.Application
	Log *zap.Logger
}

// SubscribeCheckout registers the dashboard-facing basket_create verb: mint a
// Tebex basket so the dashboard can redirect to Tebex-hosted checkout, either
// for the signed-in buyer or as a gift to another registered user.
func SubscribeCheckout(rt CheckoutRuntime, client *tebex.Client, cfg CheckoutConfig) error {
	c := &checkoutRPC{tebex: client, nc: rt.NC, userGetSubject: cfg.UserGetSubject, log: rt.Log}

	// Basket creation is two upstream HTTP calls (plus a recipient lookup for
	// gifts), so give it more room than the default in-cluster RPC budget.
	return bus.QueueSubscribeJSON[transactionsrpc.BasketCreateRequest, transactionsrpc.BasketCreateReply](
		rt.NC, cfg.Prefix+".basket_create", cfg.QueueGroup, 15*time.Second, rt.App, rt.Log, c.basketCreate)
}

// buyer is the signed-in purchaser: their numeric id and clamped display login.
type buyer struct {
	id    uint64
	login string
}

func (c *checkoutRPC) basketCreate(ctx context.Context, req transactionsrpc.BasketCreateRequest) transactionsrpc.BasketCreateReply {
	buyerID, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil || buyerID == 0 {
		return transactionsrpc.BasketCreateReply{Error: "user_id must be numeric"}
	}
	packageType, ok := normalizePackageType(req.PackageType)
	if !ok {
		return transactionsrpc.BasketCreateReply{Error: "package_type must be single or subscription"}
	}

	b := buyer{id: buyerID, login: clampLogin(req.Username)}
	spec := tebex.BasketSpec{UserID: b.id, Username: b.login, IPAddress: validIPv4(req.IPAddress), PackageType: packageType}
	recipientLogin := ""

	// A recipient turns this into a gift: the spec is rebuilt against the vetted
	// recipient with the buyer as gifter.
	if !normalizeLogin(req.RecipientUsername).empty() {
		giftSpec, recipient, errReply := c.buildGiftSpec(ctx, req, b)
		if errReply != "" {
			return transactionsrpc.BasketCreateReply{Error: errReply}
		}
		spec, recipientLogin = giftSpec, recipient
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

// normalizePackageType accepts the empty, single, or subscription package
// types; ok is false for anything else.
func normalizePackageType(raw string) (string, bool) {
	switch raw {
	case "", "single", "subscription":
		return raw, true
	default:
		return "", false
	}
}

// buildGiftSpec vets the gift recipient and assembles the gift basket spec
// (one paid month, never recurring). A non-empty errReply is the user-facing
// error to return; recipientLogin echoes the resolved recipient on success.
func (c *checkoutRPC) buildGiftSpec(ctx context.Context, req transactionsrpc.BasketCreateRequest, b buyer) (spec tebex.BasketSpec, recipientLogin, errReply string) {
	recipient := normalizeLogin(req.RecipientUsername)
	if utf8.RuneCountInString(string(recipient)) > twitchLoginMaxLen {
		// No real Twitch login is this long, so it can't belong to a registered
		// user. Reject before the lookup rather than let an oversized,
		// attacker-supplied login ride the NATS request and (on a fluke match)
		// the basket custom payload and gift email.
		return tebex.BasketSpec{}, "", errRecipientNotRegistered.Error()
	}
	view, err := c.resolveRecipient(ctx, recipient)
	if err != nil {
		return tebex.BasketSpec{}, "", err.Error()
	}
	if view.ID == b.id {
		return tebex.BasketSpec{}, "", "that is your own account — use Subscribe instead"
	}
	giftMessage := sanitizeGiftMessage(req.GiftMessage)
	if noteHasLink(giftMessage) {
		return tebex.BasketSpec{}, "", errGiftMessageLink.Error()
	}
	return tebex.BasketSpec{
		UserID:        view.ID,
		Username:      view.Username,
		IPAddress:     validIPv4(req.IPAddress),
		GiftedByID:    b.id,
		GiftedByLogin: b.login,
		PackageType:   "single",
		GiftMessage:   giftMessage,
	}, view.Username, ""
}

func validIPv4(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil || ip.To4() == nil {
		return ""
	}
	return ip.String()
}

// login is a normalized Twitch login (trimmed, lowercased, '@' dropped),
// distinct from the raw user input it is derived from.
type login string

func (l login) empty() bool { return l == "" }

// resolveRecipient vets a gift target: the Twitch login must belong to a
// registered ItsBagelBot user who is not banned and does not already have a
// paid or VIP plan. Error strings are user-facing (the dashboard surfaces them
// on the gift form verbatim).
func (c *checkoutRPC) resolveRecipient(ctx context.Context, l login) (*usersrpc.AdminUserView, error) {

	reply, err := bus.RequestJSONTimeout[usersrpc.AdminReply](ctx, c.nc, c.userGetSubject,
		usersrpc.AdminRequest{Username: string(l)}, 3*time.Second)
	if err != nil {
		if _, isReply := err.(bus.RPCReplyError); isReply {
			return nil, errRecipientNotRegistered
		}
		c.log.Warn("gift recipient lookup failed", zap.String("login", string(l)), zap.Error(err))
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
	errGiftMessageLink         = constError("gift notes can't contain links or web addresses — please remove it and try again")
)

type constError string

func (e constError) Error() string { return string(e) }

// twitchLoginMaxLen bounds a Twitch login. Real logins are 4-25 characters, so
// anything longer is junk that must not ride the recipient lookup, the Tebex
// basket custom payload, or the gift email/notification attribution.
const twitchLoginMaxLen = 25

// normalizeLogin turns user input into a Twitch login: trimmed, lowercased,
// leading @ dropped.
func normalizeLogin(input string) login {
	l := strings.TrimSpace(input)
	l = strings.TrimPrefix(l, "@")
	return login(strings.ToLower(l))
}

// clampLogin trims the buyer's display login and hard-caps it so a caller
// cannot push an oversized attribution string into the basket or gift email.
// The buyer login is display-only, so it is truncated (never rejected) to avoid
// failing a paid checkout over a cosmetic field.
func clampLogin(input string) string {
	trimmed := strings.TrimSpace(input)
	if utf8.RuneCountInString(trimmed) <= twitchLoginMaxLen {
		return trimmed
	}
	return string([]rune(trimmed)[:twitchLoginMaxLen])
}

// giftMessageMaxRunes bounds the note before it rides the Tebex custom payload.
const giftMessageMaxRunes = 280

// noteHasLink reports whether a sanitized gift note carries a link. Gift notes
// are emailed to another user, so a link (or any obfuscated form of one) is
// refused rather than delivered. See internal/domain/validate.ContainsLink.
func noteHasLink(sanitized string) bool {
	return sanitized != "" && validate.ContainsLink(sanitized)
}

// sanitizeGiftMessage cleans the buyer's optional gift note: control characters
// are dropped (newlines survive as the email preserves line breaks), the result
// is trimmed and hard-capped so an oversized or hostile note cannot bloat the
// basket. HTML escaping happens at render time in the mail package, not here.
func sanitizeGiftMessage(input string) string {

	cleaned := strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, input)

	cleaned = strings.TrimSpace(cleaned)
	if utf8.RuneCountInString(cleaned) <= giftMessageMaxRunes {
		return cleaned
	}

	runes := []rune(cleaned)
	return strings.TrimSpace(string(runes[:giftMessageMaxRunes]))
}
