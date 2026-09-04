// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package bootstrap is outgress's one-time startup REST work: learning the
// bot's own application id and registering the slash-command catalog.
// Ported from app/dingress/internal/community/slash.go's slashCatalog and
// Bot.Ready, moved here because outgress -- not ingress -- is the one
// process left with a REST client after the split (see
// internal/discordapi's GetCurrentApplication doc for why this no longer
// waits on the gateway's READY payload).
package bootstrap

import (
	"context"

	"ItsBagelBot/internal/discordapi"
)

// AppRegistrar is the REST slice this package needs.
type AppRegistrar interface {
	GetCurrentApplication(ctx context.Context) (discordapi.Snowflake, error)
	BulkOverwriteCommands(ctx context.Context, cat discordapi.CommandCatalog) error
}

// Register learns the bot's application id and registers the slash-command
// catalog. It returns the application id (also needed for interaction
// followups, see ../commands.Handlers) even on a registration failure, since
// the id itself was still learned successfully.
func Register(ctx context.Context, rest AppRegistrar) (applicationID string, err error) {
	app, err := rest.GetCurrentApplication(ctx)
	if err != nil {
		return "", err
	}
	err = rest.BulkOverwriteCommands(ctx, discordapi.CommandCatalog{
		ApplicationID: app.ID,
		Commands:      Catalog(),
	})
	return app.ID, err
}

// Catalog is the bot's slash-command catalog, unchanged from community's
// slashCatalog().
func Catalog() []discordapi.AppCommand {
	user := discordapi.AppCommandOption{Type: 6, Name: "user", Description: "Member", Required: true}
	return []discordapi.AppCommand{
		{
			Name: "ticket", Description: "Support tickets",
			Options: []discordapi.AppCommandOption{
				{Type: 1, Name: "open", Description: "Open a private ticket"},
				{Type: 1, Name: "close", Description: "Close this ticket"},
				{Type: 1, Name: "panel", Description: "Post the ticket button"},
			},
		},
		{
			Name: "voice", Description: "Manage your temporary voice channel",
			Options: []discordapi.AppCommandOption{
				{Type: 1, Name: "name", Description: "Rename", Options: []discordapi.AppCommandOption{
					{Type: 3, Name: "name", Description: "New name", Required: true},
				}},
				{Type: 1, Name: "limit", Description: "User limit", Options: []discordapi.AppCommandOption{
					{Type: 4, Name: "count", Description: "Max users", Required: true},
				}},
				{Type: 1, Name: "lock", Description: "Lock the channel"},
				{Type: 1, Name: "unlock", Description: "Unlock the channel"},
			},
		},
		{Name: "timeout", Description: "Timeout a member", Options: []discordapi.AppCommandOption{
			user,
			{Type: 4, Name: "minutes", Description: "Duration", Required: true},
		}},
		{Name: "kick", Description: "Kick a member", Options: []discordapi.AppCommandOption{user}},
		{Name: "ban", Description: "Ban a member", Options: []discordapi.AppCommandOption{user}},
		{Name: "purge", Description: "Bulk-delete messages", Options: []discordapi.AppCommandOption{
			{Type: 4, Name: "count", Description: "2–100", Required: true},
		}},
		{Name: "daily", Description: "Claim daily crumbs"},
		{Name: "rank", Description: "Show crumb rank", Options: []discordapi.AppCommandOption{
			{Type: 6, Name: "user", Description: "Member"},
		}},
	}
}
