// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpcclient

import (
	"context"
	"strings"
	"time"

	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const timeout = 8 * time.Second

const (
	ticketOpenTimeout  = discordoutgress.TicketOpenClientTimeout
	ticketCloseTimeout = discordoutgress.TicketCloseClientTimeout
)

type Client struct {
	nc     *nats.Conn
	prefix string
}

func New(nc *nats.Conn, prefix string) *Client {
	return &Client{nc: nc, prefix: strings.TrimSuffix(prefix, ".")}
}

func (c *Client) subject(name string) string { return c.prefix + "." + name }

func (c *Client) CreateChannel(ctx context.Context, req discordoutgress.ChannelCreateRequest) (discordoutgress.ChannelCreateReply, error) {
	return bus.RequestJSONTimeout[discordoutgress.ChannelCreateReply](ctx, c.nc, c.subject("channel.create"), req, timeout)
}

func (c *Client) DeleteChannel(ctx context.Context, req discordoutgress.ChannelDeleteRequest) (discordoutgress.ChannelDeleteReply, error) {
	return bus.RequestJSONTimeout[discordoutgress.ChannelDeleteReply](ctx, c.nc, c.subject("channel.delete"), req, timeout)
}

func (c *Client) ModifyChannel(ctx context.Context, req discordoutgress.ChannelModifyRequest) (discordoutgress.ChannelModifyReply, error) {
	return bus.RequestJSONTimeout[discordoutgress.ChannelModifyReply](ctx, c.nc, c.subject("channel.modify"), req, timeout)
}

func (c *Client) MoveMember(ctx context.Context, req discordoutgress.MemberMoveRequest) (discordoutgress.MemberMoveReply, error) {
	return bus.RequestJSONTimeout[discordoutgress.MemberMoveReply](ctx, c.nc, c.subject("member.move"), req, timeout)
}

func (c *Client) Purge(ctx context.Context, req discordoutgress.PurgeRequest) (discordoutgress.PurgeReply, error) {
	return bus.RequestJSONTimeout[discordoutgress.PurgeReply](ctx, c.nc, c.subject("channel.purge"), req, timeout)
}

func (c *Client) LiveOnline(ctx context.Context, req discordoutgress.LiveOnlineRequest) (discordoutgress.LiveOnlineReply, error) {
	return bus.RequestJSONTimeout[discordoutgress.LiveOnlineReply](ctx, c.nc, c.subject("live.online"), req, timeout)
}

func (c *Client) LiveOffline(ctx context.Context, req discordoutgress.LiveOfflineRequest) (discordoutgress.LiveOfflineReply, error) {
	return bus.RequestJSONTimeout[discordoutgress.LiveOfflineReply](ctx, c.nc, c.subject("live.offline"), req, timeout)
}

func (c *Client) ResolveInvite(ctx context.Context, req discordoutgress.InviteResolveRequest) (discordoutgress.InviteResolveReply, error) {
	return bus.RequestJSONTimeout[discordoutgress.InviteResolveReply](ctx, c.nc, c.subject("invite.resolve"), req, timeout)
}

func (c *Client) TicketOpen(ctx context.Context, req discordoutgress.TicketOpenRequest) (discordoutgress.TicketOpenReply, error) {
	return bus.RequestJSONTimeout[discordoutgress.TicketOpenReply](ctx, c.nc, c.subject("ticket.open"), req, ticketOpenTimeout)
}

func (c *Client) TicketClaim(ctx context.Context, req discordoutgress.TicketClaimRequest) (discordoutgress.TicketClaimReply, error) {
	return bus.RequestJSONTimeout[discordoutgress.TicketClaimReply](ctx, c.nc, c.subject("ticket.claim"), req, ticketOpenTimeout)
}

func (c *Client) TicketAddMember(ctx context.Context, req discordoutgress.TicketMemberAddRequest) (discordoutgress.TicketMemberAddReply, error) {
	return bus.RequestJSONTimeout[discordoutgress.TicketMemberAddReply](ctx, c.nc, c.subject("ticket.add"), req, ticketOpenTimeout)
}

func (c *Client) TicketClose(ctx context.Context, req discordoutgress.TicketCloseRequest) (discordoutgress.TicketCloseReply, error) {
	return bus.RequestJSONTimeout[discordoutgress.TicketCloseReply](ctx, c.nc, c.subject("ticket.close"), req, ticketCloseTimeout)
}

func (c *Client) TicketPanel(ctx context.Context, req discordoutgress.TicketPanelRequest) (discordoutgress.TicketPanelReply, error) {
	return bus.RequestJSONTimeout[discordoutgress.TicketPanelReply](ctx, c.nc, c.subject("ticket.panel"), req, ticketOpenTimeout)
}
