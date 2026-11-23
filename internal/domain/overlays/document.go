// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlays

import (
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/codec"
)

const Version = 2

type Kind string

const (
	KindSystem Kind = "system"
	KindCustom Kind = "custom"
)

type Template string

const TemplatePowered Template = "powered"

type WidgetKind string

const (
	WidgetAlertbox  WidgetKind = "alertbox"
	WidgetEventlist WidgetKind = "eventlist"
	WidgetGoal      WidgetKind = "goal"
	WidgetLabel     WidgetKind = "label"
	WidgetChatbox   WidgetKind = "chatbox"
	WidgetEmotewall WidgetKind = "emotewall"
	WidgetTimer     WidgetKind = "timer"
	WidgetCredits   WidgetKind = "credits"
	WidgetText      WidgetKind = "text"
	WidgetImage     WidgetKind = "image"
	WidgetVideo     WidgetKind = "video"
	WidgetSlideshow WidgetKind = "slideshow"
	WidgetCustom    WidgetKind = "custom"
)

var WidgetKinds = []WidgetKind{
	WidgetAlertbox, WidgetEventlist, WidgetGoal, WidgetLabel, WidgetChatbox, WidgetEmotewall, WidgetTimer,
	WidgetCredits, WidgetText, WidgetImage, WidgetVideo, WidgetSlideshow, WidgetCustom,
}

type Document struct {
	Version  int                        `json:"version"`
	Kind     Kind                       `json:"kind"`
	ID       string                     `json:"id"`
	Name     string                     `json:"name"`
	Width    int                        `json:"width,omitempty"`
	Height   int                        `json:"height,omitempty"`
	Rev      int64                      `json:"rev"`
	Template Template                   `json:"template,omitempty"`
	Config   *overlaysrpc.PoweredConfig `json:"config,omitempty"`
	Widgets  []Widget                   `json:"widgets,omitempty"`
	Assets   map[string]Asset           `json:"assets,omitempty"`
}

type Widget struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Kind     WidgetKind     `json:"kind"`
	X        float64        `json:"x"`
	Y        float64        `json:"y"`
	W        float64        `json:"w"`
	H        float64        `json:"h"`
	Rotation float64        `json:"rotation"`
	Opacity  float64        `json:"opacity"`
	Hidden   bool           `json:"hidden"`
	Locked   bool           `json:"locked"`
	Settings map[string]any `json:"settings"`
}

type Asset struct {
	Type       AssetType `json:"type"`
	Bytes      int64     `json:"bytes"`
	W          int       `json:"w"`
	H          int       `json:"h"`
	DurationMS int       `json:"duration_ms"`
	SHA256     string    `json:"sha256"`
	URL        string    `json:"url"`
}

func Parse(data []byte) (Document, error) {
	if len(data) > DocumentMaxBytes {
		return Document{}, fail(CodeDocumentTooLarge, "")
	}
	var doc Document
	if !decodeStrict(data, &doc) {
		return Document{}, fail(CodeInvalidDocument, "")
	}
	return doc, Validate(doc)
}

func decodeStrict(data []byte, into any) bool {
	return codec.UnmarshalStrict(data, into) == nil
}
