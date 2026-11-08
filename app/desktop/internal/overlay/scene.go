// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlay

import (
	"errors"
	"math"
	"slices"

	"ItsBagelBot/pkg/codec"
)

const (
	maxSceneBytes = 256 << 10
	maxLayers     = 100
	maxMessages   = 50
)

type Message struct {
	User    string `json:"user"`
	Tone    string `json:"tone"`
	Message string `json:"message"`
}

type Layer struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Name     string    `json:"name"`
	X        float64   `json:"x"`
	Y        float64   `json:"y"`
	W        float64   `json:"w"`
	H        float64   `json:"h"`
	Opacity  float64   `json:"opacity"`
	Content  string    `json:"content"`
	FontSize float64   `json:"fontSize"`
	Color    string    `json:"color"`
	Align    string    `json:"align"`
	Caption  string    `json:"caption"`
	Fit      string    `json:"fit"`
	Title    string    `json:"title"`
	Subtitle string    `json:"subtitle"`
	Tone     string    `json:"tone"`
	Rows     int       `json:"rows"`
	Messages []Message `json:"messages"`
}

type Scene struct {
	Resolution string  `json:"resolution"`
	Layers     []Layer `json:"layers"`
}

func parseScene(raw string) (Scene, error) {
	var scene Scene
	if len(raw) > maxSceneBytes {
		return scene, errors.New("overlay exceeds 256 KiB")
	}
	if err := codec.UnmarshalStrict([]byte(raw), &scene); err != nil {
		return scene, err
	}
	if err := scene.validate(); err != nil {
		return scene, err
	}
	scene.normalize()
	return scene, nil
}

func (s Scene) validate() error {
	switch s.Resolution {
	case "720p", "1080p", "2k":
	default:
		return errors.New("invalid resolution")
	}
	if len(s.Layers) > maxLayers {
		return errors.New("maximum 100 layers")
	}
	ids := make(map[string]struct{}, len(s.Layers))
	for i := range s.Layers {
		layer := &s.Layers[i]
		if _, exists := ids[layer.ID]; exists || layer.ID == "" {
			return errors.New("invalid layer identity")
		}
		ids[layer.ID] = struct{}{}
		if err := layer.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (l *Layer) validGeometry() bool {
	return between(l.X, 0, 100) && between(l.Y, 0, 100) && between(l.W, 3, 100) && between(l.H, 3, 100) &&
		l.X+l.W <= 100.001 && l.Y+l.H <= 100.001 && between(l.Opacity, 0, 100)
}

func (l *Layer) validate() error {
	if !l.validGeometry() {
		return errors.New("invalid layer geometry")
	}
	switch l.Kind {
	case "text":
		return l.validateText()
	case "image":
		if l.Fit != "contain" && l.Fit != "cover" {
			return errors.New("invalid image fit")
		}
	case "alert":
		if !validTone(l.Tone) {
			return errors.New("invalid alert tone")
		}
	case "chat":
		return l.validateChat()
	default:
		return errors.New("unknown layer kind")
	}
	return nil
}

func (l *Layer) validateText() error {
	if !validColor(l.Color) || !between(l.FontSize, 0.01, 100) {
		return errors.New("invalid text style")
	}
	switch l.Align {
	case "start", "center", "end":
		return nil
	default:
		return errors.New("invalid text alignment")
	}
}

func (l *Layer) validateChat() error {
	if l.Rows < 1 || l.Rows > maxMessages || len(l.Messages) > maxMessages {
		return errors.New("maximum 50 chat rows")
	}
	for i := range l.Messages {
		tone := l.Messages[i].Tone
		if tone != "none" && !validTone(tone) {
			return errors.New("invalid chat tone")
		}
	}
	return nil
}

func validTone(tone string) bool           { return tone == "free" || tone == "paid" || tone == "vip" }
func between(value, min, max float64) bool { return !math.IsNaN(value) && value >= min && value <= max }
func validColor(color string) bool {
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	for i := 1; i < len(color); i++ {
		c := color[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func (s Scene) clone() Scene {
	s.Layers = slices.Clone(s.Layers)
	for i := range s.Layers {
		s.Layers[i].Messages = slices.Clone(s.Layers[i].Messages)
	}
	return s
}

// Keep the wire shape compatible with the frontend's required arrays, including
// an empty chat layer and older saved documents that omitted empty messages.
func (s *Scene) normalize() {
	if s.Layers == nil {
		s.Layers = []Layer{}
	}
	for i := range s.Layers {
		if s.Layers[i].Kind == "chat" && s.Layers[i].Messages == nil {
			s.Layers[i].Messages = []Message{}
		}
	}
}
