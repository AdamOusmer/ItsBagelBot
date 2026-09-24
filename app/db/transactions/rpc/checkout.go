// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/giveawayaward"
	"ItsBagelBot/app/db/transactions/ent/giveawayuserlease"
	"ItsBagelBot/app/db/transactions/tebex"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	transactionsrpc "ItsBagelBot/internal/domain/rpc/transactions"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"
)

type checkoutRPC struct {
	tebex          *tebex.Client
	nc             *nats.Conn
	userGetSubject string
	log            *zap.Logger
	guard          *CheckoutGuard
}

type CheckoutConfig struct {
	Prefix         string
	UserGetSubject string
	Guard          *CheckoutGuard
}

type CoverageReader interface {
	Coverage(context.Context, uint64) (usersrpc.PremiumCoverage, error)
}
type AwardReader interface {
	HasPendingOrActiveAward(context.Context, uint64) (bool, error)
}
type CheckoutLease interface {
	AcquireUserLease(context.Context, uint64) (func(), error)
}

type CheckoutGuard struct {
	Coverage CoverageReader
	Awards   AwardReader
	Lease    CheckoutLease
}

func NewCheckoutGuard(db *ent.Client, coverage CoverageReader) *CheckoutGuard {
	return &CheckoutGuard{Coverage: coverage, Awards: entAwardReader{db: db}, Lease: entCheckoutLease{db: db}}
}

func (g *CheckoutGuard) Allow(ctx context.Context, userID uint64) error {
	release, err := g.Begin(ctx, userID)
	if err != nil {
		return err
	}
	release()
	return nil
}

func (g *CheckoutGuard) Begin(ctx context.Context, userID uint64) (func(), error) {
	if !g.ready() {
		return nil, errors.New("premium coverage guard unavailable")
	}
	release := func() {}
	if g.Lease != nil {
		var err error
		release, err = g.Lease.AcquireUserLease(ctx, userID)
		if err != nil {
			return nil, errors.New("could not serialize premium coverage check")
		}
	}
	if err := g.check(ctx, userID); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func (g *CheckoutGuard) check(ctx context.Context, userID uint64) error {
	if err := g.checkAward(ctx, userID); err != nil {
		return err
	}
	coverage, err := g.Coverage.Coverage(ctx, userID)
	if err != nil || coverage.BillingUncertain {
		return errors.New("could not verify premium coverage")
	}
	if blockedAccount(coverage) {
		return errAlreadyPremium
	}
	if hasFutureCoverage(coverage, time.Now().UTC()) {
		return errAlreadyPremium
	}
	return nil
}

func blockedAccount(coverage usersrpc.PremiumCoverage) bool {
	return coverage.Banned || strings.EqualFold(coverage.Status, "vip")
}

func (g *CheckoutGuard) ready() bool {
	return g != nil && g.Coverage != nil && g.Awards != nil
}

func (g *CheckoutGuard) checkAward(ctx context.Context, userID uint64) error {
	covered, err := g.Awards.HasPendingOrActiveAward(ctx, userID)
	if err != nil {
		return errors.New("could not verify premium coverage")
	}
	if covered {
		return errAlreadyPremium
	}
	return nil
}

func hasFutureCoverage(coverage usersrpc.PremiumCoverage, now time.Time) bool {
	if coverage.PaidThrough != nil && coverage.PaidThrough.After(now) {
		return true
	}
	for _, grant := range coverage.Grants {
		if grant.EndAt.After(now) {
			return true
		}
	}
	return false
}

type entAwardReader struct{ db *ent.Client }

type entCheckoutLease struct{ db *ent.Client }

func (r entCheckoutLease) AcquireUserLease(ctx context.Context, userID uint64) (func(), error) {
	if userID == 0 {
		return nil, errors.New("invalid user id")
	}
	req := checkoutLeaseRequest{userID: userID, owner: uuid.NewString(), now: time.Now().UTC()}
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return nil, err
	}
	row, findErr := checkoutLeaseRow(ctx, tx, userID)
	if err = req.save(ctx, tx, row, findErr); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return func() {
		now := time.Now().UTC()
		_, _ = r.db.GiveawayUserLease.UpdateOneID("user:" + fmt.Sprint(userID)).Where(giveawayuserlease.OwnerEQ(req.owner)).SetLeaseUntil(now).SetUpdatedAt(now).Save(context.Background())
	}, nil
}

type checkoutLeaseRequest struct {
	userID uint64
	owner  string
	now    time.Time
}

func checkoutLeaseRow(ctx context.Context, tx *ent.Tx, userID uint64) (*ent.GiveawayUserLease, error) {
	row, err := tx.GiveawayUserLease.Query().Where(giveawayuserlease.UserIDEQ(userID)).ForUpdate().Only(ctx)
	if err != nil && err.Error() == "sql: SELECT .. FOR UPDATE/SHARE not supported in SQLite" {
		return tx.GiveawayUserLease.Query().Where(giveawayuserlease.UserIDEQ(userID)).Only(ctx)
	}
	return row, err
}

func (r checkoutLeaseRequest) save(ctx context.Context, tx *ent.Tx, row *ent.GiveawayUserLease, findErr error) error {
	if findErr == nil {
		if row.LeaseUntil.After(r.now) {
			return errors.New("user checkout already in progress")
		}
		_, err := row.Update().SetOwner(r.owner).SetLeaseUntil(r.now.Add(30 * time.Second)).SetUpdatedAt(r.now).Save(ctx)
		return err
	}
	if !ent.IsNotFound(findErr) {
		return findErr
	}
	_, err := tx.GiveawayUserLease.Create().SetID("user:" + fmt.Sprint(r.userID)).SetUserID(r.userID).SetOwner(r.owner).SetLeaseUntil(r.now.Add(30 * time.Second)).SetUpdatedAt(r.now).Save(ctx)
	return err
}

func (r entAwardReader) HasPendingOrActiveAward(ctx context.Context, userID uint64) (bool, error) {
	return r.db.GiveawayAward.Query().Where(giveawayaward.UserIDEQ(userID), giveawayaward.StateIn("selected", "preparing", "needs_review", "scheduled", "active")).Exist(ctx)
}

const basketBudget = 15 * time.Second

func SubscribeCheckout(w bus.RPCWiring, client *tebex.Client, cfg CheckoutConfig) error {
	c := &checkoutRPC{tebex: client, nc: w.NC, userGetSubject: cfg.UserGetSubject, log: w.Log, guard: cfg.Guard}

	return bus.Serve(w.Within(basketBudget), cfg.Prefix+".basket_create", c.basketCreate)
}

type buyer struct {
	id    uint64
	login string
}

func (c *checkoutRPC) basketCreate(ctx context.Context, req transactionsrpc.BasketCreateRequest) transactionsrpc.BasketCreateReply {
	log := monitor.TxnLogger(ctx, c.log)
	b, packageType, err := parseBuyer(req)
	if err != nil {
		return transactionsrpc.BasketCreateReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, err.Error())}
	}
	spec, recipientLogin, release, refusal := c.buildBasket(ctx, req, b, packageType)
	if refusal != nil {
		return transactionsrpc.BasketCreateReply{Refusal: domainrpc.Refused(refusal.code, refusal.message)}
	}
	if release != nil {
		defer release()
	}

	basket, err := c.tebex.CreateBasket(ctx, spec)
	if err != nil {
		log.Warn("tebex basket create failed",
			zap.Uint64("user_id", spec.UserID), zap.Uint64("gifted_by", spec.GiftedByID), zap.Error(err))
		return transactionsrpc.BasketCreateReply{Refusal: domainrpc.Refused(domainrpc.CodeUnavailable, "checkout is unavailable right now")}
	}

	return transactionsrpc.BasketCreateReply{
		Ident:          basket.Ident,
		CheckoutURL:    basket.CheckoutURL,
		RecipientLogin: recipientLogin,
	}
}

type basketRefusal struct {
	code    domainrpc.Code
	message string
}

func parseBuyer(req transactionsrpc.BasketCreateRequest) (buyer, string, error) {
	buyerID, err := bus.UserID(req.UserID)
	if err != nil || buyerID == 0 {
		return buyer{}, "", bus.ErrInvalidUserID
	}
	packageType, ok := normalizePackageType(req.PackageType)
	if !ok {
		return buyer{}, "", errors.New("package_type must be single or subscription")
	}
	return buyer{id: buyerID, login: clampLogin(req.Username)}, packageType, nil
}

func (c *checkoutRPC) buildBasket(ctx context.Context, req transactionsrpc.BasketCreateRequest, b buyer, packageType string) (tebex.BasketSpec, string, func(), *basketRefusal) {
	if recipient := normalizeLogin(req.RecipientUsername); !recipient.empty() {
		return c.buildGiftBasket(ctx, req, b)
	}
	if c.guard != nil {
		release, err := c.guard.Begin(ctx, b.id)
		if err != nil {
			return tebex.BasketSpec{}, "", nil, &basketRefusal{code: domainrpc.CodeConflict, message: err.Error()}
		}
		return tebex.BasketSpec{UserID: b.id, Username: b.login, IPAddress: validIPv4(req.IPAddress), PackageType: packageType}, "", release, nil
	}
	return tebex.BasketSpec{UserID: b.id, Username: b.login, IPAddress: validIPv4(req.IPAddress), PackageType: packageType}, "", nil, nil
}

func (c *checkoutRPC) buildGiftBasket(ctx context.Context, req transactionsrpc.BasketCreateRequest, b buyer) (tebex.BasketSpec, string, func(), *basketRefusal) {
	spec, recipient, errReply := c.buildGiftSpec(ctx, req, b)
	if errReply != "" {
		return tebex.BasketSpec{}, "", nil, &basketRefusal{code: domainrpc.CodeInvalid, message: errReply}
	}
	if c.guard != nil {
		release, err := c.guard.Begin(ctx, spec.UserID)
		if err != nil {
			return tebex.BasketSpec{}, "", nil, &basketRefusal{code: domainrpc.CodeConflict, message: err.Error()}
		}
		return spec, recipient, release, nil
	}
	return spec, recipient, nil, nil
}

func normalizePackageType(raw string) (string, bool) {
	switch raw {
	case "", "single", "subscription":
		return raw, true
	default:
		return "", false
	}
}

func (c *checkoutRPC) buildGiftSpec(ctx context.Context, req transactionsrpc.BasketCreateRequest, b buyer) (spec tebex.BasketSpec, recipientLogin, errReply string) {
	recipient := normalizeLogin(req.RecipientUsername)
	if utf8.RuneCountInString(string(recipient)) > twitchLoginMaxLen {
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

type login string

func (l login) empty() bool { return l == "" }

func (c *checkoutRPC) resolveRecipient(ctx context.Context, l login) (*usersrpc.AdminUserView, error) {
	view, err := c.lookupRecipient(ctx, l)
	if err != nil {
		return nil, err
	}
	return c.validateRecipient(ctx, view)
}

func (c *checkoutRPC) lookupRecipient(ctx context.Context, l login) (*usersrpc.AdminUserView, error) {
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
	return reply.User, nil
}

func (c *checkoutRPC) validateRecipient(ctx context.Context, view *usersrpc.AdminUserView) (*usersrpc.AdminUserView, error) {
	if view.Banned {
		return nil, errRecipientNotEligible
	}
	switch strings.ToLower(view.Status) {
	case "paid", "vip":
		return nil, errRecipientAlreadyPremium
	}
	return view, nil
}

var (
	errRecipientNotRegistered  = constError("that user hasn't signed in to ItsBagelBot yet, so premium can't be gifted to them")
	errRecipientNotEligible    = constError("that account can't receive premium")
	errRecipientAlreadyPremium = constError("that user already has premium")
	errAlreadyPremium          = constError("that account already has premium coverage")
	errRecipientLookup         = constError("could not verify the recipient right now — try again in a moment")
	errGiftMessageLink         = constError("gift notes can't contain links or web addresses — please remove it and try again")
)

type constError string

func (e constError) Error() string { return string(e) }

const twitchLoginMaxLen = 25

func normalizeLogin(input string) login {
	l := strings.TrimSpace(input)
	l = strings.TrimPrefix(l, "@")
	return login(strings.ToLower(l))
}

func clampLogin(input string) string {
	trimmed := strings.TrimSpace(input)
	if utf8.RuneCountInString(trimmed) <= twitchLoginMaxLen {
		return trimmed
	}
	return string([]rune(trimmed)[:twitchLoginMaxLen])
}

const giftMessageMaxRunes = 280

func noteHasLink(sanitized string) bool {
	return sanitized != "" && validate.ContainsLink(sanitized)
}

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
