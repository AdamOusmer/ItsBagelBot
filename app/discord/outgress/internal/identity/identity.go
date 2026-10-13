// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package identity

import (
	_ "embed"
	"encoding/base64"
	"sync"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

//go:embed premium-avatar.png
var premiumAvatarPNG []byte

var dataURI = sync.OnceValue(func() string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(premiumAvatarPNG)
})

func PremiumAvatarDataURI() string { return dataURI() }

func Member(guildID string, want ddiscord.GuildIdentity) discapi.CurrentMember {
	m := discapi.CurrentMember{GuildID: guildID}
	if nick, ok := want.Nick(); ok {
		m.Nick = &nick
	}
	if want.Premium {
		uri := PremiumAvatarDataURI()
		m.AvatarDataURI = &uri
	}
	return m
}
