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

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/domain/rpc/discorddata"
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

// numericBroadcasterID converts a broadcaster id to the uint64 discord-data
// keys its rows by. Four write verbs opened with this same parse-or-refuse
// block; the refusal stays a plain error rather than a sentinel because a
// non-numeric id is a caller bug, not an outcome anything branches on.
func numericBroadcasterID(b Broadcaster) (uint64, error) {
	id, err := strconv.ParseUint(b.ID, 10, 64)
	if err != nil {
		return 0, errors.New("discordstore: broadcaster id must be numeric")
	}
	return id, nil
}

// ackReply is the part of a write verb's reply this client actually reads.
//
// It decodes in place of the verb's own reply type on the ack path below.
// The verbs answer with extra fields (ticket_id, opener_id) that path has no
// reader for, and the codec drops what the target struct does not declare, so
// one shape covers binding.set, binding.delete, ticket.claim, ticket.close and
// transcript.put instead of five near-identical bodies. A verb whose extra
// fields DO get read -- config.set's version, ticket.open's count -- keeps its
// own reply type and its own body.
type ackReply struct {
	rpc.Refusal
}

// ack runs a verb whose only answer is whether it worked, and classifies the
// reply through replyError so a coded refusal arrives as its sentinel.
func (s *rpcStore) ack(ctx context.Context, verb string, req any) error {
	reply, err := request[ackReply](ctx, s.rpc, s.subject(verb), req)
	if err != nil {
		return err
	}
	return replyError(reply.Error, reply.Code)
}

// memberCallFailed reports whether a member-scoped read failed, in transport
// or in the reply, logging it once. The ticket-count and the three XP verbs
// all answer a zero value on failure and had each spelled the same
// err/reply.Error/log triple.
func (s *rpcStore) memberCallFailed(verb string, m Member, err error, replyErr string) bool {
	if err == nil && replyErr == "" {
		return false
	}
	s.log.Error("discord-data "+verb+" failed", zap.String("guild_id", m.GuildID), zap.Error(err))
	return true
}

// TicketsDurable shadows the embedded local store's answer. Tickets opened
// through this store get a discord-data row: an id, an enforced open limit and
// a transcript. A discord-data that is DOWN surfaces per call, as an error the
// open path already rolls the channel back on -- not as a mode where the desk
// pretends the limit does not exist.
func (*rpcStore) TicketsDurable(context.Context) bool { return true }

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
	b, _, ok := s.BindingOf(ctx, g)
	return b, ok
}

// BindingOf is Broadcaster with the provenance of the answer. Callers deciding
// ownership refuse BindingFromCache; callers routing an event ignore it, which
// is what Broadcaster above spells.
func (s *rpcStore) BindingOf(ctx context.Context, g Guild) (Broadcaster, BindingSource, bool) {
	reply, err := request[discorddata.BindingGetReply](ctx, s.rpc, s.subject(discorddata.VerbBindingGet),
		discorddata.BindingGetRequest{GuildID: g.ID})
	if err != nil {
		s.log.Error("discord-data binding.get failed; serving the cached binding",
			zap.String("guild_id", g.ID), zap.Error(err))
		cached, ok := s.cachedBroadcaster(ctx, g)
		return cached, BindingFromCache, ok
	}
	if !reply.Found {
		// A guild that really has no binding must not keep answering from a
		// stale cache entry after an unbind this replica did not see.
		s.dropBroadcaster(ctx, g)
		return Broadcaster{}, BindingFromStore, false
	}
	b := Broadcaster{ID: strconv.FormatUint(reply.BroadcasterID, 10)}
	s.cacheBroadcaster(ctx, g, b)
	return b, BindingFromStore, true
}

// BindGuild binds a guild to a broadcaster. It fails loudly: no cache write
// happens unless discord-data confirmed the row.
func (s *rpcStore) BindGuild(ctx context.Context, bind Binding) error {
	broadcasterID, err := numericBroadcasterID(bind.Broadcaster)
	if err != nil {
		return err
	}
	err = s.ack(ctx, discorddata.VerbBindingSet, discorddata.BindingSetRequest{
		GuildID:       bind.Guild.ID,
		BroadcasterID: broadcasterID,
		InstalledBy:   bind.InstalledBy,
	})
	if err != nil {
		return err
	}
	s.cacheBroadcaster(ctx, bind.Guild, bind.Broadcaster)
	s.dropGuilds(ctx, bind.Broadcaster)
	return nil
}

// UnbindGuild removes a binding, dropping the cache entry only once
// discord-data confirmed the delete.
// The broadcaster travels with the delete so discord-data's owner guard can
// fire: without it a stale unbind for a guild that has since been re-bound to
// somebody else drops the new owner's row.
func (s *rpcStore) UnbindGuild(ctx context.Context, bind Binding) error {
	broadcasterID, err := numericBroadcasterID(bind.Broadcaster)
	if err != nil {
		return err
	}
	err = s.ack(ctx, discorddata.VerbBindingDelete,
		discorddata.BindingDeleteRequest{GuildID: bind.Guild.ID, BroadcasterID: broadcasterID})
	if err != nil {
		return err
	}
	s.dropBroadcaster(ctx, bind.Guild)
	s.dropGuilds(ctx, bind.Broadcaster)
	return nil
}

// GuildsOf lists every guild the broadcaster installed the bot into, through a
// short Valkey cache symmetric with Broadcaster's: both write verbs drop the
// key, so the TTL only covers an invalidation this replica never saw.
//
// A failure is ErrStoreUnavailable, never an empty slice. Every caller's next
// step is a loop, and an empty loop is indistinguishable from "this streamer
// connected no servers" -- which silently posts nothing on the Twitch fan-out
// and shows an empty picker on the dashboard, both of which read as a
// deliberate answer rather than as an outage.
func (s *rpcStore) GuildsOf(ctx context.Context, b Broadcaster) ([]Binding, error) {
	broadcasterID, err := numericBroadcasterID(b)
	if err != nil {
		return nil, err
	}
	if cached, ok := s.cachedGuilds(ctx, b); ok {
		return cached, nil
	}
	reply, err := request[discorddata.BindingListByBroadcasterReply](ctx, s.rpc,
		s.subject(discorddata.VerbBindingListByBroadcaster),
		discorddata.BindingListByBroadcasterRequest{BroadcasterID: broadcasterID})
	if err != nil || reply.Error != "" {
		s.log.Error("discord-data binding.list_by_broadcaster failed",
			zap.String("broadcaster_id", b.ID), zap.String("reply_error", reply.Error), zap.Error(err))
		return nil, ErrStoreUnavailable
	}
	out := make([]Binding, 0, len(reply.Guilds))
	for _, binding := range reply.Guilds {
		out = append(out, Binding{
			Guild:         Guild{ID: binding.GuildID},
			Broadcaster:   b,
			InstalledBy:   binding.InstalledBy,
			BoundAtUnixMs: binding.BoundAtUnixMs,
		})
	}
	s.cacheGuilds(ctx, b, out)
	return out, nil
}

// GuildConfig reads one guild's settings through the Valkey cache in front of
// discord-data.
//
// The fallback matches Broadcaster's, and for the same reason: a data-service
// blip must not stop every gateway event in every guild, and settings that
// changed inside the last minute are a far smaller wrong than that. The write
// path below fails loudly instead.
func (s *rpcStore) GuildConfig(ctx context.Context, g Guild) (ddiscord.Config, int, bool) {
	reply, err := request[discorddata.ConfigGetReply](ctx, s.rpc, s.subject(discorddata.VerbConfigGet),
		discorddata.ConfigGetRequest{GuildID: g.ID})
	if err != nil || reply.Error != "" {
		s.log.Error("discord-data config.get failed; serving the cached settings",
			zap.String("guild_id", g.ID), zap.String("reply_error", reply.Error), zap.Error(err))
		return s.cachedConfig(ctx, g)
	}
	if !reply.Found {
		// A guild whose settings were deleted must not keep answering from a
		// cache entry this replica's invalidation never reached.
		s.dropConfig(ctx, g)
		return ddiscord.Config{}, 0, false
	}
	s.cacheConfig(ctx, g, reply.Config, reply.Version)
	return reply.Config, reply.Version, true
}

// SetGuildConfig writes one guild's settings. The cache entry is dropped
// rather than refreshed: the reply carries only the version, and re-deriving
// the stored blob from the request would cache whatever the caller sent even
// if discord-data normalized it.
func (s *rpcStore) SetGuildConfig(ctx context.Context, set SetConfig) (int, error) {
	broadcasterID, err := numericBroadcasterID(set.Broadcaster)
	if err != nil {
		return 0, err
	}
	reply, err := request[discorddata.ConfigSetReply](ctx, s.rpc, s.subject(discorddata.VerbConfigSet),
		discorddata.ConfigSetRequest{
			GuildID:         set.Guild.ID,
			BroadcasterID:   broadcasterID,
			Config:          set.Config,
			ExpectedVersion: set.ExpectedVersion,
		})
	if err != nil {
		return 0, err
	}
	if err := replyError(reply.Error, reply.Code); err != nil {
		return 0, err
	}
	s.dropConfig(ctx, set.Guild)
	return reply.Version, nil
}

// Invalidate drops one guild's cached settings.
func (s *rpcStore) Invalidate(ctx context.Context, g Guild) { s.dropConfig(ctx, g) }

// TrackTicket records a newly opened ticket, with the guild's per-member cap
// enforced by discord-data inside the same transaction that inserts the row.
// CodeLimit is a refusal, not an error: the caller answers the opener with the
// count instead of logging a failure.
func (s *rpcStore) TrackTicket(ctx context.Context, t TicketOpen) (TicketOpenResult, error) {
	reply, err := request[discorddata.TicketOpenReply](ctx, s.rpc, s.subject(discorddata.VerbTicketOpen),
		discorddata.TicketOpenRequest{
			GuildID: t.GuildID, ChannelID: t.ChannelID, OpenerID: t.OpenerID, Subject: t.Subject,
			PanelMessageID: t.PanelMessageID, OpenLimit: t.OpenLimit,
		})
	if err != nil {
		return TicketOpenResult{}, err
	}
	if reply.Code == discorddata.CodeLimit {
		return TicketOpenResult{OpenCount: reply.OpenCount, AtLimit: true}, nil
	}
	if err := replyError(reply.Error, reply.Code); err != nil {
		return TicketOpenResult{}, err
	}
	return TicketOpenResult{TicketID: reply.TicketID, OpenCount: reply.OpenCount}, nil
}

// Ticket resolves the ticket a button press belongs to. A transport failure is
// (zero, false): the caller's next step is an ephemeral "this is not a ticket
// channel", which is the right answer to give when the store cannot say.
func (s *rpcStore) Ticket(ctx context.Context, g Guild, ch Channel) (Ticket, bool) {
	reply, err := request[discorddata.TicketGetReply](ctx, s.rpc, s.subject(discorddata.VerbTicketGet),
		discorddata.TicketGetRequest{GuildID: g.ID, ChannelID: ch.ID})
	if err != nil {
		s.log.Error("discord-data ticket.get failed", zap.String("channel_id", ch.ID), zap.Error(err))
		return Ticket{}, false
	}
	if !reply.Found {
		return Ticket{}, false
	}
	return Ticket{
		ID:             reply.Ticket.ID,
		ChannelID:      reply.Ticket.ChannelID,
		GuildID:        reply.Ticket.GuildID,
		OpenerID:       reply.Ticket.OpenerID,
		Status:         reply.Ticket.Status,
		ClaimedBy:      reply.Ticket.ClaimedBy,
		PanelMessageID: reply.Ticket.PanelMessageID,
	}, true
}

// OpenTicketCount asks discord-data how many live tickets the member holds. A
// transport failure answers zero, which lets the open through: the row insert
// still carries the limit and refuses there, so the worst case is one ticket
// past the cap rather than a desk that stops working when the RPC blips.
func (s *rpcStore) OpenTicketCount(ctx context.Context, m Member) int {
	reply, err := request[discorddata.TicketCountReply](ctx, s.rpc, s.subject(discorddata.VerbTicketCount),
		discorddata.TicketCountRequest{GuildID: m.GuildID, OpenerID: m.UserID})
	if s.memberCallFailed(discorddata.VerbTicketCount, m, err, reply.Error) {
		return 0
	}
	return reply.Count
}

// ClaimTicket records who is handling the ticket.
func (s *rpcStore) ClaimTicket(ctx context.Context, c TicketClaim) error {
	return s.ack(ctx, discorddata.VerbTicketClaim,
		discorddata.TicketClaimRequest{GuildID: c.GuildID, ChannelID: c.ChannelID, StaffID: c.StaffID})
}

// CloseTicket ends the ticket in c. The row survives: the desk, the transcript
// and the audit trail all need the history the Valkey key it replaces used to
// throw away.
func (s *rpcStore) CloseTicket(ctx context.Context, c TicketClose) error {
	return s.ack(ctx, discorddata.VerbTicketClose,
		discorddata.TicketCloseRequest{
			GuildID: c.GuildID, ChannelID: c.ChannelID,
			ClosedBy: c.ClosedBy, ArchivedChannelID: c.ArchivedChannelID,
		})
}

// PutTranscript stores a closed ticket's rendered history. A zero TicketID
// means the close never got a row (the fallback path), and there is nothing to
// attach the body to.
func (s *rpcStore) PutTranscript(ctx context.Context, t Transcript) error {
	if t.TicketID == 0 {
		return nil
	}
	return s.ack(ctx, discorddata.VerbTranscriptPut,
		discorddata.TranscriptPutRequest{TicketID: t.TicketID, Body: t.Body, MessageCount: t.MessageCount})
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
	if s.memberCallFailed(discorddata.VerbXPAdd, m, err, reply.Error) {
		return 0, false, 0
	}
	return int(reply.XPValue), reply.LeveledUp, reply.Level
}

// ClaimDaily claims the daily bonus. The 24h window is decided by
// discord-data inside one transaction, so two simultaneous claims award once.
func (s *rpcStore) ClaimDaily(ctx context.Context, m Member) (bool, int) {
	reply, err := request[discorddata.XPDailyReply](ctx, s.rpc, s.subject(discorddata.VerbXPDaily),
		discorddata.XPDailyRequest{GuildID: m.GuildID, UserID: m.UserID, Amount: dailyXP})
	if s.memberCallFailed(discorddata.VerbXPDaily, m, err, reply.Error) {
		return false, 0
	}
	return reply.Granted, int(reply.XPValue)
}

// Rank reads one member's standing. A failure reads as an unranked member,
// matching what the Valkey store did on a cache miss.
func (s *rpcStore) Rank(ctx context.Context, m Member) (int, int) {
	reply, err := request[discorddata.XPGetReply](ctx, s.rpc, s.subject(discorddata.VerbXPGet),
		discorddata.XPGetRequest{GuildID: m.GuildID, UserID: m.UserID})
	if s.memberCallFailed(discorddata.VerbXPGet, m, err, reply.Error) {
		return 0, 0
	}
	return int(reply.XPValue), reply.Level
}

// replyError turns a reply's (error, code) pair into a Go error, mapping the
// one code callers branch on onto its sentinel.
func replyError(message string, code rpc.Code) error {
	switch {
	case code == discorddata.CodeBoundElsewhere:
		return ErrBoundElsewhere
	case code == discorddata.CodeConflict:
		return ErrConfigConflict
	case code == discorddata.CodeNotBound:
		return ErrNotBound
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
