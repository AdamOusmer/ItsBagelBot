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

// ErrNotBound refuses a settings read or write for a guild the caller does not
// own. An absent binding and a binding to somebody else are the same refusal,
// so a caller cannot learn which guild ids are in use by probing.
var ErrNotBound = errors.New("this Discord server is not connected to your Twitch channel")

// GuildConfigWrite is one guild's settings save. ExpectedVersion is the
// version the dashboard page loaded with.
type GuildConfigWrite struct {
	GuildID         string
	BroadcasterID   string
	Config          ddiscord.Config
	ExpectedVersion int
}

// MaxListedGuilds caps how many servers one listing describes.
//
// Twenty-five is not a Discord limit; it is the point past which the picker
// stops being a picker. Each entry costs one REST GetGuild, so an unbounded
// list also turns one dashboard load into an unbounded burst against a shared
// rate limit -- and the streamers this beta serves run one or two servers, not
// twenty-six. A broadcaster who really needs more gets a paged listing, not a
// slower page.
const MaxListedGuilds = 25

// GuildSummary is one connected server, as the dashboard's server picker
// shows it.
type GuildSummary struct {
	GuildID string
	Name    string
	// BoundAtUnixMs is when this server was connected.
	BoundAtUnixMs int64
	// BotPresent is false when Discord answers 403 or 404: the bot was kicked
	// or the server is gone. The entry is still returned, because the binding
	// still exists and the streamer needs to see it to act on it.
	BotPresent bool
}

// GuildConfig reads one guild's settings for the dashboard.
func (w *Worker) GuildConfig(ctx context.Context, req GuildSetupRequest) (ddiscord.Config, int, bool, error) {
	if err := w.requireOwnerStrict(ctx, req); err != nil {
		return ddiscord.Config{}, 0, false, err
	}
	cfg, version, found := w.store.GuildConfig(ctx, discordstore.Guild{ID: req.GuildID})
	return cfg, version, found, nil
}

// SetGuildConfig saves one guild's settings and drops the engine's cached
// copy, so a toggle the streamer just flipped takes effect on the next event
// rather than at the end of the cache TTL.
func (w *Worker) SetGuildConfig(ctx context.Context, write GuildConfigWrite) (int, error) {
	req := GuildSetupRequest{GuildID: write.GuildID, BroadcasterID: write.BroadcasterID}
	if err := w.requireOwnerStrict(ctx, req); err != nil {
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

// ListGuilds lists every server the broadcaster connected. A guild Discord
// refuses to describe is still listed, with BotPresent false: dropping it
// would hide a binding the streamer cannot then disconnect.
//
// The loop re-checks the deadline before each entry and returns what it has
// with ctx.Err(). One entry is one REST round trip, so a list of twenty
// against a slow Discord can outlive the RPC's own timeout; returning the
// first twelve and saying the list is short beats returning nothing, and
// beats a reply the caller has already given up on.
func (w *Worker) ListGuilds(ctx context.Context, broadcasterID string) ([]GuildSummary, error) {
	if w.store == nil {
		return nil, nil
	}
	guilds, err := w.store.GuildsOf(ctx, discordstore.Broadcaster{ID: broadcasterID})
	if err != nil {
		return nil, err
	}
	if len(guilds) > MaxListedGuilds {
		guilds = guilds[:MaxListedGuilds]
	}
	out := make([]GuildSummary, 0, len(guilds))
	for _, bound := range guilds {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		out = append(out, w.summarize(ctx, bound))
	}
	return out, nil
}

// summarize asks Discord for one guild's name. It uses plain GetGuild rather
// than a with_counts variant: the REST client has no such call yet, and the
// picker shows the name and the presence pill, neither of which needs the
// member count.
func (w *Worker) summarize(ctx context.Context, bound discordstore.Binding) GuildSummary {
	out := GuildSummary{GuildID: bound.Guild.ID, BoundAtUnixMs: bound.BoundAtUnixMs}
	if w.discord == nil {
		return out
	}
	got, err := w.discord.GetGuild(ctx, discapi.Guild{ID: bound.Guild.ID})
	if err != nil {
		w.logMissingGuild(bound.Guild.ID, err)
		return out
	}
	out.Name = got.Name
	out.BotPresent = true
	return out
}

// logMissingGuild separates the two reasons a guild cannot be described. A 403
// or 404 is the expected shape of "the bot was kicked" and is not worth an
// error line on every dashboard load; anything else is a real failure.
func (w *Worker) logMissingGuild(guildID string, err error) {
	if errors.Is(err, discapi.ErrForbidden) || errors.Is(err, discapi.ErrChannelNotFound) {
		return
	}
	w.log.Warn("discord guild lookup failed; listing it as absent",
		zap.String("guild_id", guildID), zap.Error(err))
}

// requireOwnerStrict is requireOwner with a missing binding refused rather
// than allowed, and reported as ErrNotBound rather than as "bound elsewhere":
// the dashboard's two cases are "this is not yours" and "someone else claimed
// this guild", and only the second is worth its own screen.
// It refuses outright when the binding came from the Valkey cache rather than
// from discord-data. The cache exists so a data-service blip does not stop
// gateway events; an ownership decision is the one place that trade is wrong,
// because a cache entry can outlive an unbind and would then answer "yes, this
// server is yours" for a server that no longer is. Reading someone else's
// settings is a worse outcome than a dashboard that says "try again".
func (w *Worker) requireOwnerStrict(ctx context.Context, req GuildSetupRequest) error {
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
	if !ok || owner.ID != req.BroadcasterID {
		return ErrNotBound
	}
	return nil
}
