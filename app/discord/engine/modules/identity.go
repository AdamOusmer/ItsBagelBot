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

type StatusReader func(ctx context.Context, broadcasterID uint64) (string, bool)

type AppliedStore interface {
	Applied(ctx context.Context, guildID string) (string, bool)
	Record(ctx context.Context, guildID, fingerprint string) error
}

type Identity struct {
	Resolve ByBroadcaster
	Status  StatusReader
	Applied AppliedStore
	Publish Publish
	Log     *zap.Logger
}

func IdentityModule(i *Identity) module.Module {
	return module.NewModule("identity").
		On(ddiscord.SubjectEventGuild, i.onGuild).
		Build()
}

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

func (i *Identity) HandleUserChanged(msg *bus.Message) error {
	var changed eventdata.UserChangedDTO
	if err := codec.Unmarshal(msg.Payload, &changed); err != nil {
		i.Log.Warn("dropping user-changed event: malformed payload", zap.Error(err))
		return nil
	}
	ctx := msg.Context()
	want := ddiscord.IdentityFor(changed.Status)
	for _, guild := range i.Resolve(ctx, changed.UserID) {
		i.applyTo(ctx, guild.Guild.ID, want)
	}
	return nil
}

func (i *Identity) applyTo(ctx context.Context, guildID string, want ddiscord.GuildIdentity) {
	if guildID == "" || !i.needsApply(ctx, guildID, want) {
		return
	}
	if err := i.Publish(ctx, cmd.SetGuildIdentity(cmd.GuildTarget(guildID), want)); err != nil {
		i.Log.Warn("discord identity publish failed",
			zap.String("guild_id", guildID), zap.Error(err))
		return
	}
	i.record(ctx, guildID, want)
}

func (i *Identity) needsApply(ctx context.Context, guildID string, want ddiscord.GuildIdentity) bool {
	prev, ok := i.Applied.Applied(ctx, guildID)
	return !ok || prev != want.Fingerprint()
}

func (i *Identity) record(ctx context.Context, guildID string, want ddiscord.GuildIdentity) {
	if err := i.Applied.Record(ctx, guildID, want.Fingerprint()); err != nil {
		i.Log.Warn("discord identity applied but not recorded",
			zap.String("guild_id", guildID), zap.Error(err))
	}
}
