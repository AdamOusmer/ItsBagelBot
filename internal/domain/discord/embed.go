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

// LiveEmbed builds the go-live announcement. thumbnailURL is typically
// the Twitch preview; watchURL is twitch.tv/<login>.
func LiveEmbed(login, title, category, thumbnailURL string, viewers int) Embed {
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
