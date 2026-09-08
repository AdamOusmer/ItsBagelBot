// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package rpc serves discord-data's request/reply surface
// (bagel.rpc.discord-data.*): guild bindings, the ticket desk and member XP.
// Callers are discord-engine and discord-outgress, which reach it through
// internal/discordstore.NewRPC rather than by hand.
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
	BindingListByBroadcaster(ctx context.Context, broadcasterID uint64) ([]*ent.GuildBinding, error)
}

// ConfigStore is the per-guild settings half of the repository.
type ConfigStore interface {
	ConfigGet(ctx context.Context, guildID string) (ddiscord.Config, int, bool, error)
	ConfigSet(ctx context.Context, p repository.SetConfigParams) (int, error)
}

// TicketStore is the ticket-desk half of the repository.
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
	ConfigStore
	TicketStore
	XPStore
}

// Wiring is everything Subscribe needs, travelling as one value so Prefix and
// the queue group -- both strings, both plausible in either position -- cannot
// be transposed at a call site. The shared handle set is embedded rather than
// re-declared so this package's helpers can hand it straight to pkg/bus.
type Wiring struct {
	bus.RPCWiring
	Repo   Store
	Prefix string
}

// Subscribe registers every verb of the service. Fatal-on-error is the
// caller's job; this returns the first failure. The handler bound is set here
// rather than left to the caller: requestTimeout is a property of these verbs,
// not of whoever wires them up.
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

// subject builds one verb's full subject.
func (w Wiring) subject(verb string) string { return w.Prefix + "." + verb }

// serve registers one verb with the wiring every verb shares. The subscribe
// functions used to spell the seven-argument QueueSubscribeJSON call out per
// verb, eight times in a row for the ticket desk alone; this local wrapper now
// only adds the prefix, and bus.Serve keeps the queue group, timeout and
// logger in one place for every service.
func serve[Req, Rep any](w Wiring, verb string, h func(context.Context, Req) Rep) error {
	return bus.Serve(w.RPCWiring, w.subject(verb), h)
}

// refusing is any reply that embeds discorddata.Refusal.
type refusing = domainrpc.Refusing

// reply is the shape every data verb answers with: an error becomes the
// refusal, otherwise hit renders the success reply. lookup adds the miss
// case for reads that may find nothing. The handlers used to spell these
// three branches out per verb, which is what CodeScene flagged as eight
// copies of the same block.
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

// storeRules is this service's half of the classification: the six repository
// sentinels, three of which name a distinction no other service makes. The
// generic cases (a timed-out dependency, an unrecognised error) live in
// domainrpc.Fail so all seven db services answer the same way for them.
var storeRules = []domainrpc.Rule{
	domainrpc.Is(repository.ErrBoundElsewhere, discorddata.CodeBoundElsewhere),
	domainrpc.Is(repository.ErrNotBound, discorddata.CodeNotBound),
	domainrpc.Is(repository.ErrOpenLimit, discorddata.CodeLimit),
	domainrpc.Is(repository.ErrNotFound, discorddata.CodeNotFound),
	domainrpc.Is(repository.ErrInvalidInput, discorddata.CodeInvalid),
	domainrpc.Is(repository.ErrVersionConflict, discorddata.CodeConflict),
}

// refusal classifies one repository error. Kept as a named wrapper so the
// rules table is named once rather than spread across every call site.
func refusal(err error) discorddata.Refusal {
	return domainrpc.Fail(err, storeRules...)
}

// failure is refusal split into the pair the reply types that carry Error and
// Code as separate fields still take.
func failure(err error) (string, domainrpc.Code) {
	r := refusal(err)
	return r.Error, r.Code
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
