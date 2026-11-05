// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"net/url"
	"strings"
)

const (
	OverlayKindText  = "text"
	OverlayKindImage = "image"
	OverlayKindVideo = "video"
	OverlayKindSound = "sound"

	OverlayWireVersion  = 1
	MaxOverlaySeconds   = 60
	MaxOverlayVolume    = 100
	MaxOverlayTextRunes = 500
	MaxOverlayURLLength = 2000

	overlayChannelPrefix = "overlay:"
	httpsPrefix          = "https://"
	templateOpen         = "{"
)

var (
	overlayShowKinds = enum{OverlayKindText, OverlayKindImage, OverlayKindVideo}
	overlayPositions = enum{"top", "center", "bottom"}
)

type OverlayShowAction struct {
	Kind     string `json:"kind"`
	Content  string `json:"content"`
	Seconds  int    `json:"seconds"`
	Position string `json:"position"`
}

type OverlaySoundAction struct {
	URL    string `json:"url"`
	Volume int    `json:"volume"`
}

// OverlayItem is the wire shape read by the dashboard overlay stream (web/dashboard/src/lib/server/overlay-wire.ts).
type OverlayItem struct {
	Version  int    `json:"v"`
	Kind     string `json:"kind"`
	Content  string `json:"content"`
	Seconds  int    `json:"seconds,omitempty"`
	Position string `json:"position,omitempty"`
	Volume   *int   `json:"volume,omitempty"`
}

func OverlayChannel(broadcasterID string) string { return overlayChannelPrefix + broadcasterID }

func OverlayKindIsMedia(kind string) bool {
	return kind == OverlayKindImage || kind == OverlayKindVideo
}

// OverlayMediaURL reports whether a rendered template is a URL an overlay client may load.
func OverlayMediaURL(raw string) bool {
	if len(raw) > MaxOverlayURLLength {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return false
	}
	return u.Scheme == "https" && u.Hostname() != ""
}

func checkOverlayShow(c chk, p OverlayShowAction) {
	c.oneOf("kind", p.Kind, overlayShowKinds)
	c.oneOf("position", p.Position, overlayPositions)
	inRange(c, "seconds", p.Seconds, between(1, MaxOverlaySeconds))
	if c.template("content", p.Content) && OverlayKindIsMedia(p.Kind) {
		checkMediaTemplate(c, "content", p.Content)
	}
}

func checkOverlaySound(c chk, p OverlaySoundAction) {
	inRange(c, "volume", p.Volume, between(0, MaxOverlayVolume))
	if c.template("url", p.URL) {
		checkMediaTemplate(c, "url", p.URL)
	}
}

// A template opening with a token renders to a URL only at run time, where the engine checks it again.
func checkMediaTemplate(c chk, f field, template string) {
	if strings.HasPrefix(template, templateOpen) {
		return
	}
	if !strings.HasPrefix(template, httpsPrefix) {
		c.add(f, CodeInvalidFormat)
	}
}
