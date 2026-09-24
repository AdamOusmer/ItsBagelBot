// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"strings"
)

type EmoteSet struct {
	codes map[string]struct{}
}

func NewEmoteSet(codes []string) *EmoteSet {
	m := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		if c == "" {
			continue
		}
		m[c] = struct{}{}
	}
	return &EmoteSet{codes: m}
}

func (e *EmoteSet) Len() int {
	if e == nil {
		return 0
	}
	return len(e.codes)
}

func (e *EmoteSet) Has(code string) bool {
	if e == nil {
		return false
	}
	_, ok := e.codes[code]
	return ok
}

type ExtraEmotes interface {
	Known(channel uint64, code string) bool
}

type extraBox struct {
	set      ExtraEmotes
	observer interface {
		Observe(channel uint64, senderID string, tokens []string)
	}
	purger interface {
		PurgeTokens(channel uint64, tokens []string)
	}
}

func (g *Gate) SetExtraEmotes(x ExtraEmotes) {
	if x == nil {
		g.extra.Store(nil)
		return
	}
	b := &extraBox{set: x}
	if o, ok := x.(interface {
		Observe(channel uint64, senderID string, tokens []string)
	}); ok {
		b.observer = o
	}
	if p, ok := x.(interface {
		PurgeTokens(channel uint64, tokens []string)
	}); ok {
		b.purger = p
	}
	g.extra.Store(b)
}

func (g *Gate) extras() *extraBox { return g.extra.Load() }

func (g *Gate) extraKnows(channel uint64, code string) bool {
	if b := g.extra.Load(); b != nil {
		return b.set.Known(channel, code)
	}
	return false
}

const emoteMajority = 0.5

func (g *Gate) emotesUnavailable() bool { return g.emotes.Load() == nil }

func (g *Gate) emoteDominant(text string, msgCodes map[string]struct{}, ch uint64) bool {
	fetched := g.emotes.Load()
	total, known := 0, 0
	for _, tok := range strings.Fields(text) {
		total++
		if g.tokenIsKnownEmote(emoteLookup{msgCodes: msgCodes, fetched: fetched, ch: ch, token: tok}) {
			known++
		}
	}
	return total > 0 && float64(known) >= emoteMajority*float64(total)
}

type emoteLookup struct {
	msgCodes map[string]struct{}
	fetched  *EmoteSet
	ch       uint64
	token    string
}

func (g *Gate) tokenIsKnownEmote(lk emoteLookup) bool {
	return spanHasCode(lk.msgCodes, lk.token) || lk.fetched.Has(lk.token) || g.extraKnows(lk.ch, lk.token)
}

func spanHasCode(msgCodes map[string]struct{}, tok string) bool {
	_, ok := msgCodes[strings.ToLower(tok)]
	return ok
}
