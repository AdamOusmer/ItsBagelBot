// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup

import (
	"context"

	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"go.uber.org/zap"
)

type DeskRepostRequest struct {
	GuildID       string
	BroadcasterID string
	ChannelID     string
	Panel         ddiscord.TicketPanelSpec
}

func (w *Worker) RepostDesk(ctx context.Context, req DeskRepostRequest) (string, error) {
	if w.discord == nil {
		return "", ErrDiscordUnavailable
	}
	setupReq := GuildSetupRequest{GuildID: req.GuildID, BroadcasterID: req.BroadcasterID}
	if err := w.requireOwnerStrict(ctx, setupReq, ownerCheck{}); err != nil {
		return "", err
	}
	previous := w.rememberedDesk(ctx, req.GuildID)
	channelID := deskChannel(req.ChannelID, previous)
	if channelID == "" {
		return "", discapi.ErrBadRequest
	}
	w.deleteDeskMessage(ctx, previous)
	return w.postDesk(ctx, req, channelID)
}

func (w *Worker) postDesk(ctx context.Context, req DeskRepostRequest, channelID string) (string, error) {
	spec := req.Panel.OrDefaults()
	msg, err := w.discord.SendPanel(ctx,
		discapi.EmbedPost{ChannelID: channelID, Embed: ddiscord.TicketPanelEmbed(spec)},
		discapi.TicketDeskButtons(spec.Button))
	if err != nil {
		return "", err
	}
	if w.store != nil {
		_ = w.store.RememberDesk(ctx, discordstore.DeskPanel{
			GuildID: req.GuildID, ChannelID: channelID, MessageID: msg.ID,
		})
	}
	return msg.ID, nil
}

func (w *Worker) rememberedDesk(ctx context.Context, guildID string) discordstore.DeskPanel {
	if w.store == nil {
		return discordstore.DeskPanel{}
	}
	panel, _ := w.store.Desk(ctx, discordstore.Guild{ID: guildID})
	return panel
}

func deskChannel(requested string, previous discordstore.DeskPanel) string {
	if requested != "" {
		return requested
	}
	return previous.ChannelID
}

func (w *Worker) deleteDeskMessage(ctx context.Context, previous discordstore.DeskPanel) {
	if previous.MessageID == "" || previous.ChannelID == "" {
		return
	}
	err := w.discord.DeleteMessage(ctx, discapi.Message{ChannelID: previous.ChannelID, ID: previous.MessageID})
	if err != nil {
		w.log.Warn("ticket desk: previous panel not deleted",
			zap.String("channel_id", previous.ChannelID), zap.Error(err))
	}
}
