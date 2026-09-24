// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"ItsBagelBot/app/db/discord/ent"
	"ItsBagelBot/app/db/discord/repository"
	ddiscord "ItsBagelBot/internal/domain/discord"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/domain/rpc/discorddata"
	"ItsBagelBot/pkg/bus"
)

const requestTimeout = 3 * time.Second

type BindingStore interface {
	BindingGet(ctx context.Context, guildID string) (uint64, bool, error)
	BindingSet(ctx context.Context, p repository.BindParams) error
	BindingDelete(ctx context.Context, guildID string, broadcasterID uint64) error
	BindingListByBroadcaster(ctx context.Context, broadcasterID uint64) ([]*ent.GuildBinding, error)
}

type ConfigStore interface {
	ConfigGet(ctx context.Context, guildID string) (ddiscord.Config, int, bool, error)
	ConfigSet(ctx context.Context, p repository.SetConfigParams) (int, error)
}

type TicketStore interface {
	TicketOpen(ctx context.Context, p repository.OpenParams) (int, int, error)
	TicketClaim(ctx context.Context, p repository.ClaimParams) (int, error)
	TicketClose(ctx context.Context, p repository.CloseParams) (int, string, error)
	TicketGet(ctx context.Context, k repository.TicketKey) (*ent.Ticket, bool, error)
	TicketOpenCount(ctx context.Context, m repository.MemberKey) (int, error)
	TicketList(ctx context.Context, p repository.ListParams) ([]*ent.Ticket, string, error)
	TranscriptPut(ctx context.Context, ticketID int, body string, messageCount int) error
	TranscriptGet(ctx context.Context, ticketID int) (*ent.TicketTranscript, bool, error)
}

type XPStore interface {
	XPGet(ctx context.Context, guildID, userID string) (*ent.MemberXP, bool, error)
	XPAdd(ctx context.Context, guildID, userID string, delta int64) (repository.XPResult, error)
	XPDaily(ctx context.Context, guildID, userID string, amount int64) (repository.XPResult, error)
	XPTop(ctx context.Context, guildID string, limit int) ([]*ent.MemberXP, error)
}

type Store interface {
	BindingStore
	ConfigStore
	TicketStore
	XPStore
}

type Wiring struct {
	bus.RPCWiring
	Repo   Store
	Prefix string
}

func Subscribe(w Wiring) error {
	w.Timeout = requestTimeout

	if err := subscribeBindings(w); err != nil {
		return err
	}
	if err := subscribeConfigs(w); err != nil {
		return err
	}
	if err := subscribeTickets(w); err != nil {
		return err
	}
	return subscribeXP(w)
}

func (w Wiring) subject(verb string) string { return w.Prefix + "." + verb }

func serve[Req, Rep any](w Wiring, verb string, h func(context.Context, Req) Rep) error {
	return bus.Serve(w.RPCWiring, w.subject(verb), h)
}

type refusing = domainrpc.Refusing

func reply[Rep any, PR interface {
	*Rep
	refusing
}](err error, hit func() Rep) Rep {
	return lookup[Rep, PR](true, err, hit)
}

func lookup[Rep any, PR interface {
	*Rep
	refusing
}](found bool, err error, hit func() Rep) Rep {
	var zero Rep
	if err != nil {
		PR(&zero).Refuse(refusal(err))
		return zero
	}
	if !found {
		return zero
	}
	return hit()
}

var storeRules = []domainrpc.Rule{
	domainrpc.Is(repository.ErrBoundElsewhere, discorddata.CodeBoundElsewhere),
	domainrpc.Is(repository.ErrNotBound, discorddata.CodeNotBound),
	domainrpc.Is(repository.ErrOpenLimit, discorddata.CodeLimit),
	domainrpc.Is(repository.ErrNotFound, discorddata.CodeNotFound),
	domainrpc.Is(repository.ErrInvalidInput, discorddata.CodeInvalid),
	domainrpc.Is(repository.ErrVersionConflict, discorddata.CodeConflict),
}

func refusal(err error) discorddata.Refusal {
	return domainrpc.Fail(err, storeRules...)
}

func failure(err error) (string, domainrpc.Code) {
	r := refusal(err)
	return r.Error, r.Code
}

func unixMs(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func unixMsPtr(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return unixMs(*t)
}
