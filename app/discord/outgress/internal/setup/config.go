// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup

import (
	"context"
	"errors"

	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"go.uber.org/zap"
)

// Absent and foreign bindings must share this error, or probing reveals which guild ids are in use.
var ErrNotBound = errors.New("this Discord server is not connected to your Twitch channel")

type GuildConfigWrite struct {
	GuildID         string
	BroadcasterID   string
	Config          ddiscord.Config
	ExpectedVersion int
}

const MaxListedGuilds = 25

type GuildSummary struct {
	GuildID       string
	Name          string
	IconURL       string
	BoundAtUnixMs int64
	MemberCount   int
	BotPresent    bool
}

func (w *Worker) GuildConfig(ctx context.Context, req GuildSetupRequest) (ddiscord.Config, int, bool, error) {
	if err := w.requireOwnerStrict(ctx, req, ownerCheck{}); err != nil {
		return ddiscord.Config{}, 0, false, err
	}
	cfg, version, found := w.store.GuildConfig(ctx, discordstore.Guild{ID: req.GuildID})
	return cfg, version, found, nil
}

func (w *Worker) SetGuildConfig(ctx context.Context, write GuildConfigWrite) (int, error) {
	req := GuildSetupRequest{GuildID: write.GuildID, BroadcasterID: write.BroadcasterID}
	if err := w.requireOwnerStrict(ctx, req, ownerCheck{}); err != nil {
		return 0, err
	}
	version, err := w.store.SetGuildConfig(ctx, discordstore.SetConfig{
		Guild:           discordstore.Guild{ID: write.GuildID},
		Broadcaster:     discordstore.Broadcaster{ID: write.BroadcasterID},
		Config:          write.Config,
		ExpectedVersion: write.ExpectedVersion,
	})
	if err != nil {
		return 0, err
	}
	w.store.Invalidate(ctx, discordstore.Guild{ID: write.GuildID})
	return version, nil
}

type GuildListing struct {
	Guilds    []GuildSummary
	Truncated bool
}

func (w *Worker) ListGuilds(ctx context.Context, broadcasterID string) (GuildListing, error) {
	if w.store == nil {
		return GuildListing{}, nil
	}
	guilds, err := w.store.GuildsOf(ctx, discordstore.Broadcaster{ID: broadcasterID})
	if err != nil {
		return GuildListing{}, err
	}
	listing := GuildListing{Truncated: len(guilds) > MaxListedGuilds}
	if listing.Truncated {
		guilds = guilds[:MaxListedGuilds]
	}
	listing.Guilds = make([]GuildSummary, 0, len(guilds))
	for _, bound := range guilds {
		if err := ctx.Err(); err != nil {
			return listing, err
		}
		listing.Guilds = append(listing.Guilds, w.summarize(ctx, bound))
	}
	return listing, nil
}

func (w *Worker) summarize(ctx context.Context, bound discordstore.Binding) GuildSummary {
	out := GuildSummary{GuildID: bound.Guild.ID, BoundAtUnixMs: bound.BoundAtUnixMs}
	if w.discord == nil {
		return out
	}
	got, err := w.discord.GetGuildWithCounts(ctx, discapi.Guild{ID: bound.Guild.ID})
	if err != nil {
		w.logMissingGuild(bound.Guild.ID, err)
		return out
	}
	out.Name = got.Name
	out.IconURL = got.IconURL()
	out.MemberCount = got.ApproximateMemberCount
	out.BotPresent = true
	return out
}

func (w *Worker) logMissingGuild(guildID string, err error) {
	if errors.Is(err, discapi.ErrForbidden) || errors.Is(err, discapi.ErrChannelNotFound) {
		return
	}
	w.log.Warn("discord guild lookup failed; listing it as absent",
		zap.String("guild_id", guildID), zap.Error(err))
}

// Every dashboard-facing verb must use this: a cached binding can outlive an unbind.
func (w *Worker) requireOwnerStrict(ctx context.Context, req GuildSetupRequest, check ownerCheck) error {
	if w.store == nil {
		return nil
	}
	if req.GuildID == "" || req.BroadcasterID == "" {
		return ErrNotBound
	}
	owner, source, ok := w.store.BindingOf(ctx, discordstore.Guild{ID: req.GuildID})
	if source == discordstore.BindingFromCache {
		return discordstore.ErrStoreUnavailable
	}
	if !ok {
		return missingStrictBinding(check)
	}
	if owner.ID != req.BroadcasterID {
		return ErrNotBound
	}
	return nil
}

func missingStrictBinding(check ownerCheck) error {
	if check.MissingOK {
		return nil
	}
	return ErrNotBound
}
