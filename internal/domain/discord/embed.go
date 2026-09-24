// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strconv"
	"time"
)

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

const LiveColor = 0xC47A3A

type LiveEmbedInput struct {
	Login        string
	Title        string
	Category     string
	ThumbnailURL string
	Viewers      int
}

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

const OfflineContent = "Stream ended."

type ClipCard struct {
	URL     string
	Clipper string
	Title   string
}

func ClipEmbed(in ClipCard) Embed {
	e := Embed{Title: "New clip", URL: in.URL, Color: LiveColor}
	if in.Title != "" {
		e.Title = in.Title
	}
	if in.Clipper != "" {
		e.Description = in.Clipper + " clipped this"
	}
	return e
}

type WelcomeCard struct {
	Display   string
	AvatarURL string
}

func WelcomeEmbed(in WelcomeCard) Embed {
	e := Embed{
		Title:       "Welcome",
		Description: in.Display + " just joined.",
		Color:       LiveColor,
	}
	if in.AvatarURL != "" {
		e.Thumbnail = &EmbedImage{URL: in.AvatarURL}
	}
	return e
}

type Goodbye struct {
	Display string
}

func GoodbyeContent(in Goodbye) string {
	return in.Display + " left."
}

func TicketPanelEmbed(spec TicketPanelSpec) Embed {
	return Embed{
		Title:       spec.Title,
		Description: spec.Body,
		Color:       spec.ColorOr(LiveColor),
		Footer:      &EmbedFooter{Text: "Bagel tickets"},
	}
}

type TicketOpened struct {
	Opener    string
	ClaimedBy string
}

func TicketOpenedEmbed(in TicketOpened) Embed {
	who := in.Opener
	if who == "" {
		who = "Someone"
	}
	return Embed{
		Title:       "Ticket",
		Description: who + " opened this ticket. Mods will reply here.",
		Color:       LiveColor,
		Footer:      &EmbedFooter{Text: ticketFooter(in.ClaimedBy)},
	}
}

func ticketFooter(claimedBy string) string {
	if claimedBy == "" {
		return "Close with the button when you are done."
	}
	return "Claimed by " + claimedBy
}

type TicketClosed struct {
	Opener       string
	Closer       string
	Duration     time.Duration
	MessageCount int
	ChannelName  string
}

func TicketClosedEmbed(in TicketClosed) Embed {
	e := Embed{
		Title:       "Ticket closed",
		Description: ticketClosedTitle(in.ChannelName),
		Color:       LiveColor,
		Fields: []EmbedField{
			{Name: "Opened by", Value: orUnknown(in.Opener), Inline: true},
			{Name: "Closed by", Value: orUnknown(in.Closer), Inline: true},
			{Name: "Open for", Value: HumanDuration(in.Duration), Inline: true},
		},
	}
	e.Fields = append(e.Fields, EmbedField{Name: "Messages", Value: strconv.Itoa(in.MessageCount), Inline: true})
	return e
}

func ticketClosedTitle(channelName string) string {
	if channelName == "" {
		return "A ticket was closed."
	}
	return "#" + channelName + " was closed."
}

func orUnknown(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}

func HumanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	hours := int(d / time.Hour)
	minutes := int(d/time.Minute) % 60
	if hours == 0 {
		return strconv.Itoa(minutes) + "m"
	}
	return strconv.Itoa(hours) + "h " + strconv.Itoa(minutes) + "m"
}

type VoiceRoom struct {
	Owner string
}

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

type RankCard struct {
	Who   string
	Level int
	XP    int
}

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

type DailyCard struct {
	XP    int
	Fresh bool
}

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

type LevelUp struct {
	Who   string
	Level int
}

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

type LogLine struct {
	Title string
	Body  string
}

func LogEmbed(in LogLine) Embed {
	return Embed{Title: in.Title, Description: in.Body, Color: LiveColor}
}
