// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"

	discorddata "ItsBagelBot/internal/domain/rpc/discorddata"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
)

// ErrBoundElsewhere is BindGuild's refusal when the guild (or the broadcaster)
// already belongs to a different partner. It is a sentinel rather than a
// message so callers stop matching substrings: discord-data decides this on a
// unique index and reports it as a reply code.
var ErrBoundElsewhere = errors.New("discordstore: guild is bound to a different broadcaster")

// rpcTimeout bounds one discord-data call. Every verb behind it is a single
// indexed lookup or one short transaction, and the caller is an event handler
// that must not queue behind a wedged pool.
const rpcTimeout = 3 * time.Second

// Requester is the RPC transport, narrowed to what this package needs. Tests
// substitute a scripted one; production uses NATS.
type Requester interface {
	// Request sends request as JSON on subject and decodes the JSON reply into
	// reply, which is always a pointer.
	Request(ctx context.Context, subject string, request, reply any) error
}

// rpcStore serves guild bindings, tickets and XP from discord-data, and
// everything else from the embedded node-local store. Embedding rather than
// reimplementing is the point: voice occupancy, clone tracking and the desk
// lock are ephemeral, engine-private, and gain nothing from durability.
type rpcStore struct {
	// The embedded localStore supplies TrackClone/Clone/CloneCount/
	// ForgetClone, ClaimDesk/RememberDesk and UpdateVoiceOccupancy unchanged;
	// the methods below shadow the ones that moved to MySQL.
	localStore

	rpc    Requester
	prefix string
	log    *zap.Logger
}

// NewRPC builds the store discord-engine and discord-outgress use once their
// durable state lives in discord-data: bindings, tickets and XP travel over
// bagel.rpc.discord-data.*, while voice, clone, desk and the XP cooldown stay
// on the node-local Valkey the client argument opens.
func NewRPC(nc *nats.Conn, prefix string, client valkey.Client, log *zap.Logger) Store {
	return newRPCStore(natsRequester{nc: nc}, newLocal(client), prefix, log)
}

// newRPCStore is NewRPC with the transport and the local half injected, so the
// tests can script both.
func newRPCStore(requester Requester, local localStore, prefix string, log *zap.Logger) Store {
	if log == nil {
		log = zap.NewNop()
	}
	return &rpcStore{localStore: local, rpc: requester, prefix: prefix, log: log}
}

func (s *rpcStore) subject(verb string) string { return s.prefix + "." + verb }

// Broadcaster resolves a guild to its broadcaster, through the Valkey cache in
// front of discord-data.
//
// The fallback is deliberately asymmetric with the write paths. A read that
// cannot reach discord-data serves the cached binding and logs ERROR: the
// alternative is that one data-service blip stops every gateway event in every
// guild, and a binding that changed inside the last cache TTL is a far smaller
// wrong than that. A write that cannot reach it fails loudly instead (see
// BindGuild), because acknowledging a setup that was never persisted leaves the
// dashboard showing a link that does not exist.
func (s *rpcStore) Broadcaster(ctx context.Context, g Guild) (Broadcaster, bool) {
	reply, err := request[discorddata.BindingGetReply](ctx, s.rpc, s.subject(discorddata.VerbBindingGet),
		discorddata.BindingGetRequest{GuildID: g.ID})
	if err != nil {
		s.log.Error("discord-data binding.get failed; serving the cached binding",
			zap.String("guild_id", g.ID), zap.Error(err))
		return s.cachedBroadcaster(ctx, g)
	}
	if !reply.Found {
		// A guild that really has no binding must not keep answering from a
		// stale cache entry after an unbind this replica did not see.
		s.dropBroadcaster(ctx, g)
		return Broadcaster{}, false
	}
	b := Broadcaster{ID: strconv.FormatUint(reply.BroadcasterID, 10)}
	s.cacheBroadcaster(ctx, g, b)
	return b, true
}

// BindGuild binds a guild to a broadcaster. It fails loudly: no cache write
// happens unless discord-data confirmed the row.
func (s *rpcStore) BindGuild(ctx context.Context, g Guild, b Broadcaster) error {
	broadcasterID, err := strconv.ParseUint(b.ID, 10, 64)
	if err != nil {
		return errors.New("discordstore: broadcaster id must be numeric")
	}
	reply, err := request[discorddata.BindingSetReply](ctx, s.rpc, s.subject(discorddata.VerbBindingSet),
		discorddata.BindingSetRequest{GuildID: g.ID, BroadcasterID: broadcasterID})
	if err != nil {
		return err
	}
	if err := replyError(reply.Error, reply.Code); err != nil {
		return err
	}
	s.cacheBroadcaster(ctx, g, b)
	return nil
}

// UnbindGuild removes a binding, dropping the cache entry only once
// discord-data confirmed the delete.
func (s *rpcStore) UnbindGuild(ctx context.Context, g Guild) error {
	reply, err := request[discorddata.BindingDeleteReply](ctx, s.rpc, s.subject(discorddata.VerbBindingDelete),
		discorddata.BindingDeleteRequest{GuildID: g.ID})
	if err != nil {
		return err
	}
	if err := replyError(reply.Error, reply.Code); err != nil {
		return err
	}
	s.dropBroadcaster(ctx, g)
	return nil
}

// TrackTicket records a newly opened ticket. It passes no open limit: this
// verb is the legacy channel-tracking call, and the per-member cap belongs to
// the ticket desk module, which calls ticket.open with its configured limit.
func (s *rpcStore) TrackTicket(ctx context.Context, t Ticket) error {
	reply, err := request[discorddata.TicketOpenReply](ctx, s.rpc, s.subject(discorddata.VerbTicketOpen),
		discorddata.TicketOpenRequest{GuildID: t.GuildID, ChannelID: t.ChannelID, OpenerID: t.OpenerID})
	if err != nil {
		return err
	}
	return replyError(reply.Error, reply.Code)
}

// Ticket resolves the ticket a button press belongs to. A transport failure is
// (zero, false): the caller's next step is an ephemeral "this is not a ticket
// channel", which is the right answer to give when the store cannot say.
func (s *rpcStore) Ticket(ctx context.Context, ch Channel) (Ticket, bool) {
	reply, err := request[discorddata.TicketGetReply](ctx, s.rpc, s.subject(discorddata.VerbTicketGet),
		discorddata.TicketGetRequest{ChannelID: ch.ID})
	if err != nil {
		s.log.Error("discord-data ticket.get failed", zap.String("channel_id", ch.ID), zap.Error(err))
		return Ticket{}, false
	}
	if !reply.Found {
		return Ticket{}, false
	}
	return Ticket{
		ChannelID: reply.Ticket.ChannelID,
		GuildID:   reply.Ticket.GuildID,
		OpenerID:  reply.Ticket.OpenerID,
	}, true
}

// ForgetTicket closes the ticket in ch. The row survives: the desk, the
// transcript and the audit trail all need the history the Valkey key it
// replaces used to throw away.
func (s *rpcStore) ForgetTicket(ctx context.Context, ch Channel) error {
	reply, err := request[discorddata.TicketCloseReply](ctx, s.rpc, s.subject(discorddata.VerbTicketClose),
		discorddata.TicketCloseRequest{ChannelID: ch.ID})
	if err != nil {
		return err
	}
	return replyError(reply.Error, reply.Code)
}

// AddXP credits one message's XP, taking the node-local cooldown first. A
// member inside the cooldown costs one xp.get, not one write.
func (s *rpcStore) AddXP(ctx context.Context, m Member) (int, bool, int) {
	if !s.takeXPCooldown(ctx, m) {
		xp, level := s.Rank(ctx, m)
		return xp, false, level
	}
	reply, err := request[discorddata.XPAddReply](ctx, s.rpc, s.subject(discorddata.VerbXPAdd),
		discorddata.XPAddRequest{GuildID: m.GuildID, UserID: m.UserID, Delta: xpPerMessage})
	if err != nil || reply.Error != "" {
		s.log.Error("discord-data xp.add failed", zap.String("guild_id", m.GuildID), zap.Error(err))
		return 0, false, 0
	}
	return int(reply.XPValue), reply.LeveledUp, reply.Level
}

// ClaimDaily claims the daily bonus. The 24h window is decided by
// discord-data inside one transaction, so two simultaneous claims award once.
func (s *rpcStore) ClaimDaily(ctx context.Context, m Member) (bool, int) {
	reply, err := request[discorddata.XPDailyReply](ctx, s.rpc, s.subject(discorddata.VerbXPDaily),
		discorddata.XPDailyRequest{GuildID: m.GuildID, UserID: m.UserID, Amount: dailyXP})
	if err != nil || reply.Error != "" {
		s.log.Error("discord-data xp.daily failed", zap.String("guild_id", m.GuildID), zap.Error(err))
		return false, 0
	}
	return reply.Granted, int(reply.XPValue)
}

// Rank reads one member's standing. A failure reads as an unranked member,
// matching what the Valkey store did on a cache miss.
func (s *rpcStore) Rank(ctx context.Context, m Member) (int, int) {
	reply, err := request[discorddata.XPGetReply](ctx, s.rpc, s.subject(discorddata.VerbXPGet),
		discorddata.XPGetRequest{GuildID: m.GuildID, UserID: m.UserID})
	if err != nil || reply.Error != "" {
		s.log.Error("discord-data xp.get failed", zap.String("guild_id", m.GuildID), zap.Error(err))
		return 0, 0
	}
	return int(reply.XPValue), reply.Level
}

// replyError turns a reply's (error, code) pair into a Go error, mapping the
// one code callers branch on onto its sentinel.
func replyError(message, code string) error {
	switch {
	case code == discorddata.CodeBoundElsewhere:
		return ErrBoundElsewhere
	case message != "":
		return errors.New(message)
	default:
		return nil
	}
}

// request is the typed wrapper over the transport interface. It exists because
// a method cannot be generic, and the interface has to stay substitutable.
func request[Reply any](ctx context.Context, r Requester, subject string, req any) (Reply, error) {
	var out Reply
	err := r.Request(ctx, subject, req, &out)
	return out, err
}

// natsRequester is the production transport.
type natsRequester struct{ nc *nats.Conn }

// Request performs the round trip.
//
// It does not use bus.RequestJSON, which every other RPC client here does:
// that helper turns a reply carrying a non-empty "error" field into an
// RPCReplyError BEFORE decoding the body, so the reply's `code` never reaches
// the caller. This client switches on that code (bound_elsewhere is a
// different outcome from a failed call, not a differently-worded one), so it
// decodes the whole reply and lets replyError classify it. The cost is the New
// Relic messaging segments RequestJSON adds; the call still rides
// bus.RequestMsgWithContext, so trace headers and RPC locality are unchanged.
func (n natsRequester) Request(ctx context.Context, subject string, req, reply any) error {
	body, err := codec.FastMarshal(req)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	msg := nats.NewMsg(subject)
	msg.Data = body
	response, err := bus.RequestMsgWithContext(ctx, n.nc, msg)
	if err != nil {
		return err
	}
	return codec.FastUnmarshal(response.Data, reply)
}
