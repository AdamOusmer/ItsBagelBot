// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strconv"
	"time"
)

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

// ClipCard is a compact clip archive post.
type ClipCard struct {
	URL     string
	Clipper string
	Title   string
}

// ClipEmbed is a compact clip archive post.
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

// WelcomeCard greets a joiner in #welcome. AvatarURL may be empty.
type WelcomeCard struct {
	Display   string
	AvatarURL string
}

// WelcomeEmbed greets a joiner in #welcome.
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

// Goodbye is the leave line when goodbye is on.
type Goodbye struct {
	Display string
}

// GoodbyeContent is the leave line when goodbye is on.
func GoodbyeContent(in Goodbye) string {
	return in.Display + " left."
}

// TicketPanelEmbed is the persistent support-desk message. It takes the
// resolved spec rather than reading a Config: the desk panel is rendered from
// three places (the setup fill, the engine's EnsureDesk, and the dashboard's
// desk.repost RPC), and only one of them holds a Config -- passing the already
// resolved spec is what lets the other two render the streamer's own copy
// without carrying a config reader they otherwise have no use for.
func TicketPanelEmbed(spec TicketPanelSpec) Embed {
	return Embed{
		Title:       spec.Title,
		Description: spec.Body,
		Color:       spec.ColorOr(LiveColor),
		Footer:      &EmbedFooter{Text: "Bagel tickets"},
	}
}

// TicketOpened is the card posted into a newly created ticket channel.
type TicketOpened struct {
	Opener string
	// ClaimedBy is the display name of the staff member who claimed the
	// ticket, empty while it is unclaimed. It rides the same embed (the claim
	// handler edits this message in place) so the channel shows one card whose
	// footer is the ticket's state, rather than a second card nobody reads.
	ClaimedBy string
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
		Footer:      &EmbedFooter{Text: ticketFooter(in.ClaimedBy)},
	}
}

func ticketFooter(claimedBy string) string {
	if claimedBy == "" {
		return "Close with the button when you are done."
	}
	return "Claimed by " + claimedBy
}

// TicketClosed is the close summary posted into the ticket log channel.
type TicketClosed struct {
	Opener       string
	Closer       string
	Duration     time.Duration
	MessageCount int
	ChannelName  string
}

// TicketClosedEmbed is the audit card a closed ticket leaves behind: who
// opened it, who closed it, how long it was open and how many messages the
// transcript holds.
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

// HumanDuration renders a ticket's lifetime the way a moderator reads it:
// whole minutes under an hour, hours and minutes above. Seconds are dropped
// rather than rounded up, because "0m" for a ticket opened and closed by
// accident is more honest than "1m".
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

// LogLine is one audit line in #logs.
type LogLine struct {
	Title string
	Body  string
}

// LogEmbed is one audit line in #logs.
func LogEmbed(in LogLine) Embed {
	return Embed{Title: in.Title, Description: in.Body, Color: LiveColor}
}
