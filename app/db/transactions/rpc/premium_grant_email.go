// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"strconv"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/transactions/mail"
	"ItsBagelBot/internal/domain/rpc"
	transactionsrpc "ItsBagelBot/internal/domain/rpc/transactions"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

type GrantMailer interface {
	SendGrant(context.Context, mail.GrantMessage) error
}

type GrantContacts interface {
	Authorize(context.Context, string) (uint64, error)
	Contact(context.Context, uint64) (usersrpc.EmailGetReply, error)
}

type GrantEmailConfig struct {
	Users  GrantContacts
	Mailer GrantMailer
}

type grantEmailRPC struct {
	users  GrantContacts
	mailer GrantMailer
	log    *zap.Logger
}

var errGrantEmailExpired = errors.New("expires_at must be in the future")

func SubscribeGrantEmail(w bus.RPCWiring, cfg GrantEmailConfig) error {
	g := &grantEmailRPC{users: cfg.Users, mailer: cfg.Mailer, log: w.Log}
	return bus.Serve(w.Within(adminConsoleBudget), transactionsrpc.GrantEmailSubject, g.send)
}

func (g *grantEmailRPC) send(ctx context.Context, req transactionsrpc.GrantEmailRequest) transactionsrpc.GrantEmailReply {
	if _, err := g.users.Authorize(ctx, req.ActorID); err != nil {
		return grantEmailRefused(forbidden(err))
	}
	id, err := grantEmailTarget(req)
	if err != nil {
		return grantEmailRefused(rpc.Refused(rpc.CodeInvalid, err.Error()))
	}
	contact, err := g.users.Contact(ctx, id)
	if err != nil {
		return grantEmailRefused(rpc.Refused(rpc.CodeUnavailable, "contact email lookup failed"))
	}
	if contact.Email == "" {
		return transactionsrpc.GrantEmailReply{Outcome: transactionsrpc.GrantEmailNoEmail}
	}
	if err := g.mailer.SendGrant(ctx, mail.GrantMessage{
		To: contact.Email, Locale: contact.Locale, ExpiresAt: req.ExpiresAt,
		IdempotencyKey: grantEmailKey(id, req.ExpiresAt),
	}); err != nil {
		g.log.Warn("premium grant email send failed", zap.Uint64("recipient", id), zap.Error(err))
		return grantEmailRefused(rpc.Refused(rpc.CodeUnavailable, err.Error()))
	}
	g.log.Info("premium grant email sent", zap.Uint64("recipient", id), zap.String("actor", req.ActorID))
	return transactionsrpc.GrantEmailReply{Outcome: transactionsrpc.GrantEmailSent}
}

func grantEmailTarget(req transactionsrpc.GrantEmailRequest) (uint64, error) {
	id, err := bus.UserID(req.UserID)
	if err != nil {
		return 0, err
	}
	if !req.ExpiresAt.After(time.Now()) {
		return 0, errGrantEmailExpired
	}
	return id, nil
}

func grantEmailKey(id uint64, expiresAt time.Time) string {
	return "admin-grant-" + strconv.FormatUint(id, 10) + "-" + strconv.FormatInt(expiresAt.Unix(), 10)
}

func grantEmailRefused(r rpc.Refusal) transactionsrpc.GrantEmailReply {
	return transactionsrpc.GrantEmailReply{Refusal: r}
}
