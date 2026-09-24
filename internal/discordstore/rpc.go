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

var ErrBoundElsewhere = errors.New("discordstore: guild is bound to a different broadcaster")

const rpcTimeout = 3 * time.Second

type Requester interface {
	Request(ctx context.Context, subject string, request, reply any) error
}

type rpcStore struct {
	localStore

	rpc    Requester
	prefix string
	log    *zap.Logger
}

func NewRPC(nc *nats.Conn, prefix string, client valkey.Client, log *zap.Logger) Store {
	return newRPCStore(natsRequester{nc: nc}, newLocal(client), prefix, log)
}

func newRPCStore(requester Requester, local localStore, prefix string, log *zap.Logger) Store {
	if log == nil {
		log = zap.NewNop()
	}
	return &rpcStore{localStore: local, rpc: requester, prefix: prefix, log: log}
}

func (s *rpcStore) subject(verb string) string { return s.prefix + "." + verb }

func numericBroadcasterID(b Broadcaster) (uint64, error) {
	id, err := strconv.ParseUint(b.ID, 10, 64)
	if err != nil {
		return 0, errors.New("discordstore: broadcaster id must be numeric")
	}
	return id, nil
}

type ackReply struct {
	rpc.Refusal
}

func (s *rpcStore) ack(ctx context.Context, verb string, req any) error {
	reply, err := request[ackReply](ctx, s.rpc, s.subject(verb), req)
	if err != nil {
		return err
	}
	return replyError(reply.Error, reply.Code)
}

func (s *rpcStore) memberCallFailed(verb string, m Member, err error, replyErr string) bool {
	if err == nil && replyErr == "" {
		return false
	}
	s.log.Error("discord-data "+verb+" failed", zap.String("guild_id", m.GuildID), zap.Error(err))
	return true
}

func (*rpcStore) TicketsDurable(context.Context) bool { return true }

func (s *rpcStore) Broadcaster(ctx context.Context, g Guild) (Broadcaster, bool) {
	b, _, ok := s.BindingOf(ctx, g)
	return b, ok
}

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
		s.dropBroadcaster(ctx, g)
		return Broadcaster{}, BindingFromStore, false
	}
	b := Broadcaster{ID: strconv.FormatUint(reply.BroadcasterID, 10)}
	s.cacheBroadcaster(ctx, g, b)
	return b, BindingFromStore, true
}

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

func (s *rpcStore) GuildConfig(ctx context.Context, g Guild) (ddiscord.Config, int, bool) {
	reply, err := request[discorddata.ConfigGetReply](ctx, s.rpc, s.subject(discorddata.VerbConfigGet),
		discorddata.ConfigGetRequest{GuildID: g.ID})
	if err != nil || reply.Error != "" {
		s.log.Error("discord-data config.get failed; serving the cached settings",
			zap.String("guild_id", g.ID), zap.String("reply_error", reply.Error), zap.Error(err))
		return s.cachedConfig(ctx, g)
	}
	if !reply.Found {
		s.dropConfig(ctx, g)
		return ddiscord.Config{}, 0, false
	}
	s.cacheConfig(ctx, g, reply.Config, reply.Version)
	return reply.Config, reply.Version, true
}

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

func (s *rpcStore) Invalidate(ctx context.Context, g Guild) { s.dropConfig(ctx, g) }

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

func (s *rpcStore) OpenTicketCount(ctx context.Context, m Member) int {
	reply, err := request[discorddata.TicketCountReply](ctx, s.rpc, s.subject(discorddata.VerbTicketCount),
		discorddata.TicketCountRequest{GuildID: m.GuildID, OpenerID: m.UserID})
	if s.memberCallFailed(discorddata.VerbTicketCount, m, err, reply.Error) {
		return 0
	}
	return reply.Count
}

func (s *rpcStore) ClaimTicket(ctx context.Context, c TicketClaim) error {
	return s.ack(ctx, discorddata.VerbTicketClaim,
		discorddata.TicketClaimRequest{GuildID: c.GuildID, ChannelID: c.ChannelID, StaffID: c.StaffID})
}

func (s *rpcStore) CloseTicket(ctx context.Context, c TicketClose) error {
	return s.ack(ctx, discorddata.VerbTicketClose,
		discorddata.TicketCloseRequest{
			GuildID: c.GuildID, ChannelID: c.ChannelID,
			ClosedBy: c.ClosedBy, ArchivedChannelID: c.ArchivedChannelID,
		})
}

func (s *rpcStore) PutTranscript(ctx context.Context, t Transcript) error {
	if t.TicketID == 0 {
		return nil
	}
	return s.ack(ctx, discorddata.VerbTranscriptPut,
		discorddata.TranscriptPutRequest{TicketID: t.TicketID, Body: t.Body, MessageCount: t.MessageCount})
}

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

func (s *rpcStore) ClaimDaily(ctx context.Context, m Member) (bool, int) {
	reply, err := request[discorddata.XPDailyReply](ctx, s.rpc, s.subject(discorddata.VerbXPDaily),
		discorddata.XPDailyRequest{GuildID: m.GuildID, UserID: m.UserID, Amount: dailyXP})
	if s.memberCallFailed(discorddata.VerbXPDaily, m, err, reply.Error) {
		return false, 0
	}
	return reply.Granted, int(reply.XPValue)
}

func (s *rpcStore) Rank(ctx context.Context, m Member) (int, int) {
	reply, err := request[discorddata.XPGetReply](ctx, s.rpc, s.subject(discorddata.VerbXPGet),
		discorddata.XPGetRequest{GuildID: m.GuildID, UserID: m.UserID})
	if s.memberCallFailed(discorddata.VerbXPGet, m, err, reply.Error) {
		return 0, 0
	}
	return int(reply.XPValue), reply.Level
}

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

func request[Reply any](ctx context.Context, r Requester, subject string, req any) (Reply, error) {
	var out Reply
	err := r.Request(ctx, subject, req, &out)
	return out, err
}

type natsRequester struct{ nc *nats.Conn }

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
