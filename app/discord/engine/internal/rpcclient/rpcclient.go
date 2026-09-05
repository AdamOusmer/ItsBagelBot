// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package rpcclient is engine's caller side of
// internal/domain/rpc/discordoutgress -- see that package's doc for why
// these particular operations cannot be a fire-and-forget Command. Every
// method here blocks on outgress's REST call; callers already only reach
// this package from a slash-command or button Handler, which has an
// interaction to answer regardless of how long the underlying REST call
// takes, unlike the perishable automod path.
package rpcclient

import (
	"context"
	"strings"
	"time"

	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

// timeout bounds one round trip: a channel create/delete/modify or a purge
// is a handful of REST calls at most, so this is generous rather than tight.
const timeout = 8 * time.Second

// The desk's client deadlines come from the shared table in
// internal/domain/rpc/discordoutgress/timeouts.go, which pairs each with the
// deadline outgress's handler runs under. Every one of these is LONGER than
// its server side; see that file for the outage that happens when it is not.
const (
	ticketOpenTimeout  = discordoutgress.TicketOpenClientTimeout
	ticketCloseTimeout = discordoutgress.TicketCloseClientTimeout
)

// Client calls app/discord/outgress's internal channel-management and
// go-live RPC surface.
type Client struct {
	nc     *nats.Conn
	prefix string
}

// New builds the client. prefix is app/discord/outgress's own
// NATS_DISCORD_OUTGRESS_RPC_PREFIX (default bagel.rpc.discord-outgress).
func New(nc *nats.Conn, prefix string) *Client {
	return &Client{nc: nc, prefix: strings.TrimSuffix(prefix, ".")}
}

func (c *Client) subject(name string) string { return c.prefix + "." + name }

func (c *Client) CreateChannel(ctx context.Context, req discordoutgress.ChannelCreateRequest) (discordoutgress.ChannelCreateReply, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return bus.RequestJSON[discordoutgress.ChannelCreateReply](ctx, c.nc, c.subject("channel.create"), req)
}

func (c *Client) DeleteChannel(ctx context.Context, req discordoutgress.ChannelDeleteRequest) (discordoutgress.ChannelDeleteReply, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return bus.RequestJSON[discordoutgress.ChannelDeleteReply](ctx, c.nc, c.subject("channel.delete"), req)
}

func (c *Client) ModifyChannel(ctx context.Context, req discordoutgress.ChannelModifyRequest) (discordoutgress.ChannelModifyReply, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return bus.RequestJSON[discordoutgress.ChannelModifyReply](ctx, c.nc, c.subject("channel.modify"), req)
}

func (c *Client) MoveMember(ctx context.Context, req discordoutgress.MemberMoveRequest) (discordoutgress.MemberMoveReply, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return bus.RequestJSON[discordoutgress.MemberMoveReply](ctx, c.nc, c.subject("member.move"), req)
}

func (c *Client) Purge(ctx context.Context, req discordoutgress.PurgeRequest) (discordoutgress.PurgeReply, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return bus.RequestJSON[discordoutgress.PurgeReply](ctx, c.nc, c.subject("channel.purge"), req)
}

func (c *Client) LiveOnline(ctx context.Context, req discordoutgress.LiveOnlineRequest) (discordoutgress.LiveOnlineReply, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return bus.RequestJSON[discordoutgress.LiveOnlineReply](ctx, c.nc, c.subject("live.online"), req)
}

func (c *Client) LiveOffline(ctx context.Context, req discordoutgress.LiveOfflineRequest) (discordoutgress.LiveOfflineReply, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return bus.RequestJSON[discordoutgress.LiveOfflineReply](ctx, c.nc, c.subject("live.offline"), req)
}

// ResolveInvite calls bagel.rpc.discord-outgress.invite.resolve (see
// internal/domain/rpc/discordoutgress's InviteResolveRequest doc). Callers
// are expected to have already gated this behind a tripped, invite-shaped
// linkguard Verdict and a cache miss (see
// app/discord/engine/modules/linkguard.go's ownInvite) -- this method
// itself has no notion of "only sometimes call me", it always makes the
// round trip.
func (c *Client) ResolveInvite(ctx context.Context, req discordoutgress.InviteResolveRequest) (discordoutgress.InviteResolveReply, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return bus.RequestJSON[discordoutgress.InviteResolveReply](ctx, c.nc, c.subject("invite.resolve"), req)
}

// TicketOpen creates the ticket channel and posts the opening card. See
// internal/domain/rpc/discordoutgress/ticket.go for why the desk's three
// steps are RPCs rather than Commands.
func (c *Client) TicketOpen(ctx context.Context, req discordoutgress.TicketOpenRequest) (discordoutgress.TicketOpenReply, error) {
	ctx, cancel := context.WithTimeout(ctx, ticketOpenTimeout)
	defer cancel()
	return bus.RequestJSON[discordoutgress.TicketOpenReply](ctx, c.nc, c.subject("ticket.open"), req)
}

func (c *Client) TicketClaim(ctx context.Context, req discordoutgress.TicketClaimRequest) (discordoutgress.TicketClaimReply, error) {
	ctx, cancel := context.WithTimeout(ctx, ticketOpenTimeout)
	defer cancel()
	return bus.RequestJSON[discordoutgress.TicketClaimReply](ctx, c.nc, c.subject("ticket.claim"), req)
}

func (c *Client) TicketAddMember(ctx context.Context, req discordoutgress.TicketMemberAddRequest) (discordoutgress.TicketMemberAddReply, error) {
	ctx, cancel := context.WithTimeout(ctx, ticketOpenTimeout)
	defer cancel()
	return bus.RequestJSON[discordoutgress.TicketMemberAddReply](ctx, c.nc, c.subject("ticket.add"), req)
}

// TicketClose runs the whole close sequence on outgress. It gets its own,
// longer deadline: the transcript pages a channel up to twenty times behind a
// shared rate-limit bucket, which the 8s every other call here uses cannot
// cover. The interaction has already been deferred by ingress, so the user is
// looking at a "thinking" state, not a dropped command.
func (c *Client) TicketClose(ctx context.Context, req discordoutgress.TicketCloseRequest) (discordoutgress.TicketCloseReply, error) {
	ctx, cancel := context.WithTimeout(ctx, ticketCloseTimeout)
	defer cancel()
	return bus.RequestJSON[discordoutgress.TicketCloseReply](ctx, c.nc, c.subject("ticket.close"), req)
}

// TicketPanel posts the persistent desk panel and returns its message id. It
// is an RPC rather than the PostPanel Command the desk used to emit because
// the id is the whole point: without it the desk pointer has nothing a repost
// can delete. See discordoutgress.TicketPanelRequest.
func (c *Client) TicketPanel(ctx context.Context, req discordoutgress.TicketPanelRequest) (discordoutgress.TicketPanelReply, error) {
	ctx, cancel := context.WithTimeout(ctx, ticketOpenTimeout)
	defer cancel()
	return bus.RequestJSON[discordoutgress.TicketPanelReply](ctx, c.nc, c.subject("ticket.panel"), req)
}
