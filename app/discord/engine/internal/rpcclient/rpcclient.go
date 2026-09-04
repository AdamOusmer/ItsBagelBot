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
