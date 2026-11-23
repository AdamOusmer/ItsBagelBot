// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/db/users/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

func marketingRules() []domainrpc.Rule {
	return []domainrpc.Rule{
		domainrpc.Is(repository.ErrMarketingTokenInvalid, domainrpc.CodeInvalid),
		domainrpc.Is(repository.ErrConsentInvalid, domainrpc.CodeInvalid),
		domainrpc.Is(validate.ErrEmailInvalid, domainrpc.CodeInvalid),
		domainrpc.Is(repository.ErrNoContactEmail, domainrpc.CodeInvalid),
		domainrpc.Is(repository.ErrConsentMACUnavailable, domainrpc.CodeUnavailable),
		domainrpc.Is(repository.ErrSendUnknown, domainrpc.CodeUnavailable),
	}
}

func (d *dashboardRPC) handleMarketingTokenGet(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.MarketingTokenGetRequest](msg)
	if !ok {
		return
	}
	state, err := d.repo.MarketingTokenState(ctx, req.Token)
	if err != nil {
		respondFail(msg, err, marketingRules()...)
		return
	}
	bus.Respond(msg, usersrpc.MarketingTokenGetReply{
		OK: true, Status: state.Status, MaskedEmail: d.maskedContactEmail(ctx, state.UserID), Locale: state.Locale,
	})
}

func (d *dashboardRPC) handleMarketingTokenSet(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.MarketingTokenSetRequest](msg)
	if !ok {
		return
	}
	err := d.repo.SetMarketingByToken(ctx, req.Token, repository.MarketingChoice{Status: req.Status, Source: req.Source})
	if err != nil {
		respondFail(msg, err, marketingRules()...)
		return
	}
	bus.Respond(msg, usersrpc.MarketingTokenSetReply{OK: true})
}

func SubscribeMarketing(w Wiring, prefix string) error {
	m := marketingRPC{repo: w.Repo}
	budgeted := w.Within(marketingBudget)
	if err := bus.Serve(budgeted, prefix+".suppress", m.suppress); err != nil {
		return err
	}
	return bus.Serve(budgeted, prefix+".audience", m.audience)
}

type marketingRPC struct {
	repo *repository.Users
}

func (m marketingRPC) suppress(ctx context.Context, req usersrpc.MarketingSuppressRequest) usersrpc.MarketingSuppressReply {
	if err := m.repo.SuppressAddress(ctx, req.Email, req.Reason); err != nil {
		return usersrpc.MarketingSuppressReply{Refusal: domainrpc.Fail(err, marketingRules()...)}
	}
	return usersrpc.MarketingSuppressReply{OK: true}
}

func (m marketingRPC) audience(ctx context.Context, req usersrpc.MarketingAudienceRequest) usersrpc.MarketingAudienceReply {
	page, err := m.repo.MarketingAudience(ctx, req.AfterID, req.Limit)
	if err != nil {
		return usersrpc.MarketingAudienceReply{Refusal: domainrpc.Fail(err, marketingRules()...)}
	}
	recipients := make([]usersrpc.MarketingRecipient, len(page.Recipients))
	for i, recipient := range page.Recipients {
		recipients[i] = usersrpc.MarketingRecipient{UserID: recipient.UserID, Locale: recipient.Locale}
	}
	return usersrpc.MarketingAudienceReply{Recipients: recipients, NextAfterID: page.Next}
}
