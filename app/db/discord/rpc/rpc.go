// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package rpc serves discord-data's request/reply surface
// (bagel.rpc.discord-data.*): guild bindings, the ticket desk and member XP.
// Callers are discord-engine and discord-outgress, which reach it through
// internal/discordstore.NewRPC rather than by hand.
package rpc

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/db/discord/ent"
	"ItsBagelBot/app/db/discord/repository"
	discorddata "ItsBagelBot/internal/domain/rpc/discorddata"
)

// requestTimeout bounds one handler. Three seconds matches the other data
// services: every verb here is a single indexed lookup or one short
// transaction, so anything slower is a wedged pool, not a slow query, and the
// caller is better off failing than queueing behind it.
const requestTimeout = 3 * time.Second

// BindingStore is the guild-binding half of the repository, as this package
// uses it. Declared here (consumer side) so tests can substitute a stub
// without the repository having to export an interface it does not need.
type BindingStore interface {
	BindingGet(ctx context.Context, guildID string) (uint64, bool, error)
	BindingSet(ctx context.Context, p repository.BindParams) error
	BindingDelete(ctx context.Context, guildID string, broadcasterID uint64) error
	BindingByBroadcaster(ctx context.Context, broadcasterID uint64) (string, bool, error)
}

// TicketStore is the ticket-desk half of the repository.
type TicketStore interface {
	TicketOpen(ctx context.Context, p repository.OpenParams) (int, int, error)
	TicketClaim(ctx context.Context, guildID, channelID, staffID string) (int, error)
	TicketClose(ctx context.Context, p repository.CloseParams) (int, string, error)
	TicketGet(ctx context.Context, guildID, channelID string) (*ent.Ticket, bool, error)
	TicketList(ctx context.Context, p repository.ListParams) ([]*ent.Ticket, string, error)
	TranscriptPut(ctx context.Context, ticketID int, body string, messageCount int) error
	TranscriptGet(ctx context.Context, ticketID int) (*ent.TicketTranscript, bool, error)
}

// XPStore is the member-XP half of the repository.
type XPStore interface {
	XPGet(ctx context.Context, guildID, userID string) (*ent.MemberXP, bool, error)
	XPAdd(ctx context.Context, guildID, userID string, delta int64) (repository.XPResult, error)
	XPDaily(ctx context.Context, guildID, userID string, amount int64) (repository.XPResult, error)
	XPTop(ctx context.Context, guildID string, limit int) ([]*ent.MemberXP, error)
}

// Store is the whole surface one discord-data process serves.
type Store interface {
	BindingStore
	TicketStore
	XPStore
}

// Wiring is everything Subscribe needs, travelling as one value so Prefix and
// QueueGroup -- both strings, both plausible in either position -- cannot be
// transposed at a call site.
type Wiring struct {
	NC         *nats.Conn
	Repo       Store
	Prefix     string
	QueueGroup string
	App        *newrelic.Application
	Log        *zap.Logger
}

// Subscribe registers every verb of the service. Fatal-on-error is the
// caller's job; this returns the first failure.
func Subscribe(w Wiring) error {
	if err := subscribeBindings(w); err != nil {
		return err
	}
	if err := subscribeTickets(w); err != nil {
		return err
	}
	return subscribeXP(w)
}

// subject builds one verb's full subject.
func (w Wiring) subject(verb string) string { return w.Prefix + "." + verb }

// failure maps a repository error onto the reply's (error, code) pair. The
// code is what callers switch on; the message is for logs and for the one
// release during which the console still reads text.
func failure(err error) (string, string) {
	switch {
	case err == nil:
		return "", discorddata.CodeOK
	case errors.Is(err, repository.ErrBoundElsewhere):
		return err.Error(), discorddata.CodeBoundElsewhere
	case errors.Is(err, repository.ErrNotBound):
		return err.Error(), discorddata.CodeNotBound
	case errors.Is(err, repository.ErrOpenLimit):
		return err.Error(), discorddata.CodeLimit
	case errors.Is(err, repository.ErrNotFound):
		return err.Error(), discorddata.CodeNotFound
	case errors.Is(err, repository.ErrInvalidInput):
		return err.Error(), discorddata.CodeInvalid
	default:
		return err.Error(), discorddata.CodeInternal
	}
}

// unixMs renders a timestamp for the wire. Zero times stay 0 rather than
// becoming the 1970 epoch in milliseconds, so "unset" is distinguishable.
func unixMs(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// unixMsPtr is unixMs for the nillable timestamp columns.
func unixMsPtr(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return unixMs(*t)
}
