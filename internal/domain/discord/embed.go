// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

// Embed is the Discord embed object outgress posts for go-live / clips.
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	URL         string       `json:"url,omitempty"`
	Color       int          `json:"color,omitempty"`
	Thumbnail   *EmbedImage  `json:"thumbnail,omitempty"`
	Image       *EmbedImage  `json:"image,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
}

type EmbedImage struct {
	URL string `json:"url"`
}

type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type EmbedFooter struct {
	Text string `json:"text,omitempty"`
}

// LiveColor is a warm bagel-ish amber, not a rainbow.
const LiveColor = 0xC47A3A

// LiveEmbedInput feeds LiveEmbed. ThumbnailURL is typically the Twitch
// preview; the watch link is twitch.tv/<Login>.
type LiveEmbedInput struct {
	Login        string
	Title        string
	Category     string
	ThumbnailURL string
	Viewers      int
}

// LiveEmbed builds the go-live announcement.
func LiveEmbed(in LiveEmbedInput) Embed {
	login, title, category, thumbnailURL, viewers := in.Login, in.Title, in.Category, in.ThumbnailURL, in.Viewers
	watch := "https://twitch.tv/" + login
	e := Embed{
		Title:  title,
		URL:    watch,
		Color:  LiveColor,
		Footer: &EmbedFooter{Text: "Watch on Twitch"},
	}
	if e.Title == "" {
		e.Title = login + " is live"
	}
	if category != "" {
		e.Fields = append(e.Fields, EmbedField{Name: "Category", Value: category, Inline: true})
	}
	if viewers > 0 {
		e.Fields = append(e.Fields, EmbedField{Name: "Viewers", Value: itoa(viewers), Inline: true})
	}
	if thumbnailURL != "" {
		e.Image = &EmbedImage{URL: thumbnailURL}
	}
	if e.Description == "" {
		e.Description = "Live on Twitch"
	}
	return e
}

// OfflineContent is the edit applied to the live message when the stream
// ends so Discord is not stuck on LIVE.
const OfflineContent = "Stream ended."

// ClipEmbed is a compact clip archive post.
func ClipEmbed(clipURL, clipper, title string) Embed {
	e := Embed{Title: "New clip", URL: clipURL, Color: LiveColor}
	if title != "" {
		e.Title = title
	}
	if clipper != "" {
		e.Description = clipper + " clipped this"
	}
	return e
}

// WelcomeEmbed greets a joiner in #welcome. AvatarURL may be empty.
func WelcomeEmbed(display, avatarURL string) Embed {
	e := Embed{
		Title:       "Welcome",
		Description: display + " just joined.",
		Color:       LiveColor,
	}
	if avatarURL != "" {
		e.Thumbnail = &EmbedImage{URL: avatarURL}
	}
	return e
}

// GoodbyeContent is the leave line when goodbye is on.
func GoodbyeContent(display string) string {
	return display + " left."
}

// TicketPanelEmbed is the persistent support-desk message.
func TicketPanelEmbed() Embed {
	return Embed{
		Title:       "Need help?",
		Description: "Open a private ticket with the button. Mods will see it.",
		Color:       LiveColor,
		Footer:      &EmbedFooter{Text: "Bagel tickets"},
	}
}

// TicketOpened is the card posted into a newly created ticket channel.
type TicketOpened struct {
	Opener string
}

// TicketOpenedEmbed greets the opener and points at the Close button.
func TicketOpenedEmbed(in TicketOpened) Embed {
	who := in.Opener
	if who == "" {
		who = "Someone"
	}
	return Embed{
		Title:       "Ticket",
		Description: who + " opened this ticket. Mods will reply here.",
		Color:       LiveColor,
		Footer:      &EmbedFooter{Text: "Close with the button when you are done."},
	}
}

// VoiceRoom is the control card posted into a join-to-create clone.
type VoiceRoom struct {
	Owner string
}

// VoiceRoomEmbed sits in the clone's chat with Lock and Unlock buttons.
func VoiceRoomEmbed(in VoiceRoom) Embed {
	who := in.Owner
	if who == "" {
		who = "Voice"
	}
	return Embed{
		Title:       who + "'s room",
		Description: "Lock or unlock this channel with the buttons.",
		Color:       LiveColor,
	}
}

// RankCard is one crumb rank embed.
type RankCard struct {
	Who   string
	Level int
	XP    int
}

// RankEmbed is the public rank card. Callers attach Claim daily when it is
// the caller's own rank.
func RankEmbed(card RankCard) Embed {
	who := card.Who
	if who == "" {
		who = "This member"
	}
	return Embed{
		Title:       "Rank",
		Description: who + " is level " + itoa(card.Level) + ".",
		Color:       LiveColor,
		Fields:      []EmbedField{{Name: "Crumbs", Value: itoa(card.XP), Inline: true}},
	}
}

// DailyCard is the daily-claim result.
type DailyCard struct {
	XP    int
	Fresh bool
}

// DailyEmbed is the daily crumbs card.
func DailyEmbed(card DailyCard) Embed {
	if !card.Fresh {
		return Embed{Title: "Daily crumbs", Description: "Already claimed today.", Color: LiveColor}
	}
	return Embed{
		Title:       "Daily crumbs",
		Description: "Claimed. You have " + itoa(card.XP) + " crumbs.",
		Color:       LiveColor,
	}
}

// LevelUp is a chat level-up card.
type LevelUp struct {
	Who   string
	Level int
}

// LevelUpEmbed celebrates a crumb level.
func LevelUpEmbed(in LevelUp) Embed {
	who := in.Who
	if who == "" {
		who = "Someone"
	}
	return Embed{
		Title:       "Level up",
		Description: who + " reached level " + itoa(in.Level) + ".",
		Color:       LiveColor,
	}
}

// LogEmbed is one audit line in #logs.
func LogEmbed(title, body string) Embed {
	return Embed{Title: title, Description: body, Color: LiveColor}
}
