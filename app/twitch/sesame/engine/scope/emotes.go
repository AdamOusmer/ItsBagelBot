// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"math/rand/v2"
	"strings"
)

const (
	EmotesToken        = "emotes"
	SevenTVEmotesToken = "7tvemotes"
	BTTVEmotesToken    = "bttvemotes"
	FFZEmotesToken     = "ffzemotes"
	RandomEmoteToken   = "random.emote"
)

var emoteProviders = map[string]string{
	"7tv":  SevenTVEmotesToken,
	"bttv": BTTVEmotesToken,
	"ffz":  FFZEmotesToken,
}

const MaxEmoteLine = 480

const MaxEmoteDraws = 3

type EmoteSets struct {
	SevenTV []string
	BTTV    []string
	FFZ     []string
}

type EmoteSource interface {
	Emotes() EmoteSets
}

type Emotes struct {
	Source EmoteSource
	Draws  int
	Pick   func(n int) int
}

func (Emotes) Owns(v Var) bool {
	switch v.Name {
	case EmotesToken, SevenTVEmotesToken, BTTVEmotesToken, FFZEmotesToken, RandomEmoteToken:
		return true
	}
	return false
}

func emoteFamily(tok Var) (string, bool) {
	switch tok.Name {
	case SevenTVEmotesToken, BTTVEmotesToken, FFZEmotesToken:
		return tok.Name, !tok.HasPayload
	case EmotesToken:
		if !tok.HasPayload {
			return "", false
		}
		name, ok := emoteProviders[strings.ToLower(tok.Payload)]
		return name, ok
	}
	return "", false
}

func (e Emotes) Plan(_ context.Context, wants []Var) (Values, error) {
	sets := e.snapshot()
	vals := &emoteValues{draws: e.drawCodes(sets)}
	for _, want := range wants {
		if family, ok := emoteFamily(want); ok {
			vals.fill(family, sets)
		}
	}
	return vals, nil
}

func (e Emotes) snapshot() EmoteSets {
	if e.Source == nil {
		return EmoteSets{}
	}
	return e.Source.Emotes()
}

func (e Emotes) drawCodes(sets EmoteSets) []string {
	if e.Draws <= 0 {
		return nil
	}
	pool := drawPool(sets)
	if len(pool) == 0 {
		return nil
	}
	wanted := min(e.Draws, MaxEmoteDraws)
	draws := make([]string, 0, wanted)
	for i := 0; i < wanted; i++ {
		draws = append(draws, pool[e.pick(len(pool))])
	}
	return draws
}

func drawPool(sets EmoteSets) []string {
	pool := make([]string, 0, len(sets.SevenTV)+len(sets.BTTV)+len(sets.FFZ))
	pool = append(pool, sets.SevenTV...)
	pool = append(pool, sets.BTTV...)
	return append(pool, sets.FFZ...)
}

func (e Emotes) pick(n int) int {
	if e.Pick != nil {
		return e.Pick(n)
	}
	return rand.IntN(n)
}

type emoteValues struct {
	sevenTV string
	bttv    string
	ffz     string
	draws   []string
	drawn   int
}

func (v *emoteValues) fill(name string, sets EmoteSets) {
	switch name {
	case SevenTVEmotesToken:
		v.sevenTV = joinEmoteLine(sets.SevenTV)
	case BTTVEmotesToken:
		v.bttv = joinEmoteLine(sets.BTTV)
	case FFZEmotesToken:
		v.ffz = joinEmoteLine(sets.FFZ)
	}
}

func (v *emoteValues) Get(tok Var) (string, bool) {
	if family, ok := emoteFamily(tok); ok {
		return v.list(family), true
	}
	if tok.Name == RandomEmoteToken && !tok.HasPayload {
		return v.nextDraw(), true
	}
	return "", false
}

func (v *emoteValues) list(family string) string {
	switch family {
	case SevenTVEmotesToken:
		return v.sevenTV
	case BTTVEmotesToken:
		return v.bttv
	case FFZEmotesToken:
		return v.ffz
	}
	return ""
}

func (v *emoteValues) nextDraw() string {
	if len(v.draws) == 0 {
		return ""
	}
	code := v.draws[min(v.drawn, len(v.draws)-1)]
	v.drawn++
	return code
}

func joinEmoteLine(codes []string) string {
	var b strings.Builder
	for _, code := range codes {
		if b.Len()+len(code)+1 > MaxEmoteLine {
			break
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(code)
	}
	return b.String()
}
