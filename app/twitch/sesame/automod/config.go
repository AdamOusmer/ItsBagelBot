// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"bytes"
	"strings"

	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/codec"
)

type Level uint8

const (
	LevelModerate Level = iota
	LevelNone
	LevelBasic
	LevelStrict
)

func parseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "none", "off", "floor":
		return LevelNone
	case "basic", "adult", "18+", "mature":
		return LevelBasic
	case "strict", "all", "pg", "family":
		return LevelStrict
	default:
		return LevelModerate
	}
}

func (l Level) String() string {
	switch l {
	case LevelNone:
		return "none"
	case LevelBasic:
		return "basic"
	case LevelStrict:
		return "strict"
	default:
		return "moderate"
	}
}

type sections struct {
	harassment bool
	sexual     bool
	profanity  bool
	style      bool
	links      bool
	clipsOnly  bool
	capsThresh float64
}

func levelSections(l Level) sections {
	switch l {
	case LevelNone:
		return sections{capsThresh: capsThreshold}
	case LevelBasic:
		return sections{harassment: true, capsThresh: capsThreshold}
	case LevelStrict:
		return sections{harassment: true, sexual: true, profanity: true, style: true, links: true, capsThresh: 0.6}
	default:
		return sections{harassment: true, sexual: true, style: true, links: true, capsThresh: capsThreshold}
	}
}

type triState uint8

const (
	triUnset triState = iota
	triOn
	triOff
)

func parseTri(s string) triState {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "1", "yes":
		return triOn
	case "off", "false", "0", "no":
		return triOff
	default:
		return triUnset
	}
}

func (t triState) apply(def bool) bool {
	switch t {
	case triOn:
		return true
	case triOff:
		return false
	default:
		return def
	}
}

type Config struct {
	Disabled bool
	Level    Level

	harassment, sexual, profanity, style, links, clipsOnly triState

	blockTerms [][]byte
	allowTerms [][]byte
}

type wireConfig struct {
	Level      string `json:"level"`
	Profile    string `json:"profile"`
	Harassment string `json:"harassment"`
	Sexual     string `json:"sexual"`
	Profanity  string `json:"profanity"`
	Style      string `json:"style"`
	Links      string `json:"links"`
	ClipsOnly  string `json:"clips_only"`
	BlockTerms string `json:"block_terms"`
	AllowTerms string `json:"allow_terms"`
}

func ParseConfig(raw codec.RawMessage) *Config {
	if len(raw) == 0 {
		return nil
	}
	var w wireConfig
	if err := codec.Unmarshal(raw, &w); err != nil {
		return nil
	}
	lvl := w.Level
	if lvl == "" {
		lvl = w.Profile
	}
	return &Config{
		Level:      parseLevel(lvl),
		harassment: parseTri(w.Harassment),
		sexual:     parseTri(w.Sexual),
		profanity:  parseTri(w.Profanity),
		style:      parseTri(w.Style),
		links:      parseTri(w.Links),
		clipsOnly:  parseTri(w.ClipsOnly),
		blockTerms: normalizeTerms(splitTerms(w.BlockTerms)),
		allowTerms: normalizeTerms(splitTerms(w.AllowTerms)),
	}
}

func (c *Config) resolved() sections {
	if c == nil {
		return levelSections(LevelModerate)
	}
	if c.Disabled {
		return levelSections(LevelNone)
	}
	s := levelSections(c.Level)
	s.harassment = c.harassment.apply(s.harassment)
	s.sexual = c.sexual.apply(s.sexual)
	s.profanity = c.profanity.apply(s.profanity)
	s.style = c.style.apply(s.style)
	s.links = c.links.apply(s.links)
	s.clipsOnly = c.clipsOnly.apply(s.clipsOnly)
	return s
}

func splitTerms(s string) []string {
	if s == "" {
		return nil
	}
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' })
	out := fields[:0]
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func normalizeTerms(terms []string) [][]byte {
	if len(terms) == 0 {
		return nil
	}
	out := make([][]byte, 0, len(terms))
	for _, t := range terms {
		n := moderation.Normalize(nil, t)
		if len(n) > 0 {
			out = append(out, n)
		}
	}
	return out
}

func (c *Config) disabled() bool { return c != nil && c.Disabled }

func (c *Config) hasBlockTerms() bool {
	return c != nil && !c.Disabled && len(c.blockTerms) > 0
}

func (c *Config) clipsOnlyOn() bool {
	return c.resolved().clipsOnly
}

func (c *Config) allows(skel []byte) bool {
	if c == nil || c.Disabled {
		return false
	}
	return containsAny(skel, c.allowTerms)
}

func containsAny(skel []byte, terms [][]byte) bool {
	for _, t := range terms {
		if bytes.Contains(skel, t) {
			return true
		}
	}
	return false
}
