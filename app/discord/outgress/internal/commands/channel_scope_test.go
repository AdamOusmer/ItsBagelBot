// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"context"
	"errors"
	"testing"
)

func TestChannelCommandsRejectForeignGuildBeforeREST(t *testing.T) {
	for _, kind := range []string{ddiscord.TypePostChat, ddiscord.TypePostEmbed, ddiscord.TypePostPanel, ddiscord.TypeEditMessage, ddiscord.TypeDeleteMessage} {
		rest := &fakeRest{channelGuilds: map[string]string{"c2": "g2"}}
		h := &Handlers{Rest: rest}
		err := h.Dispatch(context.Background(), ddiscord.Command{Type: kind, GuildID: "g1", ChannelID: "c2"})
		if !errors.Is(err, discapi.ErrForbidden) {
			t.Fatalf("%s: %v", kind, err)
		}
		if len(rest.chats)+len(rest.embeds)+len(rest.panels)+len(rest.edited)+len(rest.deleted) != 0 {
			t.Fatal("foreign REST side effect")
		}
	}
}
