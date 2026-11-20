// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

func ownerWrite[Req interface {
	OwnerContext() usersrpc.DashboardOwner
}](write func(context.Context, uint64, Req) error) func(context.Context, *nats.Msg) {
	return func(ctx context.Context, msg *nats.Msg) {
		req, ok := decodeRequest[Req](msg)
		if !ok {
			return
		}
		id, ok := dashboardOwnerID(msg, req.OwnerContext())
		if !ok {
			return
		}
		respondConsentWrite(msg, write(ctx, id, req))
	}
}

func (d *dashboardRPC) setConsent(ctx context.Context, id uint64, req usersrpc.ConsentSetRequest) error {
	return d.repo.SetConsent(ctx, id, repository.TermsConsent{
		TermsVersion: req.TermsVersion, Marketing: req.Marketing, Source: req.Source,
		Locale: req.Locale, RequestID: req.RequestID,
	})
}

func (d *dashboardRPC) setMarketing(ctx context.Context, id uint64, req usersrpc.MarketingSetRequest) error {
	return d.repo.SetMarketing(ctx, id, repository.MarketingChoice{Status: req.Status, Source: req.Source, Locale: req.Locale})
}

func dashboardOwnerID(msg *nats.Msg, owner usersrpc.DashboardOwner) (uint64, bool) {
	if !isDashboardOwner(owner) {
		bus.Respond(msg, domainrpc.Refused(domainrpc.CodeForbidden, "account owner required"))
		return 0, false
	}
	return parseWireID(msg, owner.BroadcasterUserID, "broadcaster_user_id")
}

func isDashboardOwner(owner usersrpc.DashboardOwner) bool {
	if owner.DelegateOf != "" || owner.ImpersonatorID != "" {
		return false
	}
	return owner.ActorID != "" && owner.ActorID == owner.BroadcasterUserID
}

func respondConsentWrite(msg *nats.Msg, err error) {
	if err != nil {
		respondFail(msg, err,
			domainrpc.Is(validate.ErrUserIDZero, domainrpc.CodeInvalid),
			domainrpc.Is(repository.ErrConsentInvalid, domainrpc.CodeInvalid),
			domainrpc.When(ent.IsValidationError, domainrpc.CodeInvalid),
			domainrpc.Is(repository.ErrNoContactEmail, domainrpc.CodeInvalid),
			domainrpc.Is(repository.ErrConsentMACUnavailable, domainrpc.CodeUnavailable),
			domainrpc.Is(repository.ErrConsentRequestUsed, domainrpc.CodeConflict),
		)
		return
	}
	respondOK(msg)
}
