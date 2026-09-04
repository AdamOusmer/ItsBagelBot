// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	"ItsBagelBot/app/discord/engine/module"
	ddiscord "ItsBagelBot/internal/domain/discord"
	eventdata "ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// StatusReader returns a broadcaster's projected account status ("free",
// "paid", "vip"). Matches the shape of the projection read the engine
// already does elsewhere, so main can pass a method value.
type StatusReader func(ctx context.Context, broadcasterID uint64) (string, bool)

// AppliedStore is the per-guild memory of the appearance already applied.
// Implemented by internal/identitystore.
type AppliedStore interface {
	Applied(ctx context.Context, guildID string) (string, bool)
	Record(ctx context.Context, guildID, fingerprint string) error
}

// Identity keeps the bot's per-guild appearance in step with the streamer's
// tier: a premium server shows "ItsBagelBot - Premium" with the premium
// avatar, a free one shows the bot's global identity.
//
// It has two triggers, deliberately, because neither alone is sufficient:
//
//   - data.users.changed makes an upgrade or downgrade visible immediately,
//     and carries the new status in the event itself, so no lookup is needed.
//   - GUILD_CREATE covers everything the event cannot: a guild that was set
//     up while the engine was down, an apply that failed, a server that
//     installed the bot after the streamer's last tier change. It fires on
//     every connect, which is exactly why the applied-fingerprint check
//     matters (see identitystore's package doc).
type Identity struct {
	Resolve ByBroadcaster
	Status  StatusReader
	Applied AppliedStore
	Publish Publish
	Log     *zap.Logger
}

// IdentityModule registers the GUILD_CREATE reconciliation pass.
func IdentityModule(i *Identity) module.Module {
	return module.NewModule("identity").
		On(ddiscord.SubjectEventGuild, i.onGuild).
		Build()
}

// onGuild reconciles the guild the bot just connected to. The status comes
// from the projection rather than the event, because a gateway event knows
// nothing about Twitch tiers.
func (i *Identity) onGuild(ctx context.Context, c *module.Context, emit module.Emit) error {
	if c.Config.GuildID == "" {
		return nil
	}
	id, err := strconv.ParseUint(c.BroadcasterID, 10, 64)
	if err != nil {
		return nil
	}
	status, ok := i.Status(ctx, id)
	if !ok {
		// No projected account: premium cannot be told from free, and
		// guessing "free" would strip a paying streamer's badge on every
		// reconnect while the projection is briefly cold. Leaving the
		// appearance alone is the safe direction.
		return nil
	}
	want := ddiscord.IdentityFor(status)
	if !i.needsApply(ctx, c.Config.GuildID, want) {
		return nil
	}
	emit(cmd.SetGuildIdentity(cmd.GuildTarget(c.Config.GuildID), want))
	i.record(ctx, c.Config.GuildID, want)
	return nil
}

// HandleUserChanged applies an appearance change the moment a tier does.
// Always returns nil (ack): a malformed payload is dropped, and a failed
// apply is retried by the next GUILD_CREATE rather than by redelivering an
// account event other consumers have already handled.
func (i *Identity) HandleUserChanged(msg *bus.Message) error {
	var changed eventdata.UserChangedDTO
	if err := codec.Unmarshal(msg.Payload, &changed); err != nil {
		i.Log.Warn("dropping user-changed event: malformed payload", zap.Error(err))
		return nil
	}
	ctx := msg.Context()
	cfg, ok := i.Resolve(ctx, changed.UserID)
	if !ok || cfg.GuildID == "" {
		return nil
	}
	want := ddiscord.IdentityFor(changed.Status)
	if !i.needsApply(ctx, cfg.GuildID, want) {
		return nil
	}
	if err := i.Publish(ctx, cmd.SetGuildIdentity(cmd.GuildTarget(cfg.GuildID), want)); err != nil {
		i.Log.Warn("discord identity publish failed",
			zap.String("guild_id", cfg.GuildID), zap.Error(err))
		return nil
	}
	i.record(ctx, cfg.GuildID, want)
	return nil
}

// needsApply reports whether the guild is not already wearing want. A cache
// miss means apply: an unknown guild and one known to be on the default
// appearance need different actions, and only one is a no-op.
func (i *Identity) needsApply(ctx context.Context, guildID string, want ddiscord.GuildIdentity) bool {
	prev, ok := i.Applied.Applied(ctx, guildID)
	return !ok || prev != want.Fingerprint()
}

// record remembers the appearance now published for guildID. It runs only
// after the command is on its way, so a publish failure retries on the next
// trigger instead of being remembered as done. It records that the COMMAND
// was published, not that Discord accepted it: outgress owns that half, and
// a failed REST call nacks onto the work queue and is redelivered there.
func (i *Identity) record(ctx context.Context, guildID string, want ddiscord.GuildIdentity) {
	if err := i.Applied.Record(ctx, guildID, want.Fingerprint()); err != nil {
		// The command is already sent, so the appearance will be correct;
		// only the memory of it failed. The consequence is a redundant
		// re-apply on the next connect, not anything user-visible.
		i.Log.Warn("discord identity applied but not recorded",
			zap.String("guild_id", guildID), zap.Error(err))
	}
}
