// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"math/rand/v2"
	"strconv"
)

const (
	ChattersToken      = "chatters"
	RandomChatterToken = "random.chatter"
	RandomViewerToken  = "random.viewer"
)

const MaxChatterDraws = 3

type Chatter struct {
	ID   uint64
	Name string
}

type Roster interface {
	Chatters() []Chatter
}

type Viewers interface {
	Viewers(ctx context.Context) ([]Chatter, bool)
}

type Chatters struct {
	Roster  Roster
	Exclude []uint64
	Draws   int
	Pick    func(n int) int

	Viewers       Viewers
	ViewerExclude []uint64
	ViewerDraws   int
}

func (Chatters) Owns(v Var) bool {
	return v.Name == ChattersToken || v.Name == RandomChatterToken || v.Name == RandomViewerToken
}

func (c Chatters) Plan(ctx context.Context, _ []Var) (Values, error) {
	roster := c.snapshot()
	out := &chatterValues{
		count: len(roster),
		draws: c.drawFrom(chatterDrawPool(roster, c.Exclude), c.Draws),
	}
	if c.ViewerDraws > 0 && c.Viewers != nil {
		if viewers, ok := c.Viewers.Viewers(ctx); ok {
			out.viewerDraws = c.drawFrom(chatterDrawPool(viewers, c.ViewerExclude), c.ViewerDraws)
		}
	}
	return out, nil
}

func (c Chatters) snapshot() []Chatter {
	if c.Roster == nil {
		return nil
	}
	return c.Roster.Chatters()
}

func (c Chatters) drawFrom(names []string, n int) []string {
	if len(names) == 0 {
		return nil
	}
	wanted := min(n, MaxChatterDraws)
	draws := make([]string, 0, wanted)
	for i := 0; i < wanted; i++ {
		draws = append(draws, names[c.pick(len(names))])
	}
	return draws
}

func (c Chatters) pick(n int) int {
	if c.Pick != nil {
		return c.Pick(n)
	}
	return rand.IntN(n)
}

func chatterDrawPool(entries []Chatter, exclude []uint64) []string {
	out := make([]string, 0, len(entries))
	for _, who := range entries {
		if who.Name == "" || idExcluded(exclude, who.ID) {
			continue
		}
		out = append(out, who.Name)
	}
	return out
}

func idExcluded(list []uint64, id uint64) bool {
	for _, skip := range list {
		if id == skip {
			return true
		}
	}
	return false
}

type chatterValues struct {
	count       int
	draws       []string
	drawn       int
	viewerDraws []string
	viewerDrawn int
}

func (v *chatterValues) Get(tok Var) (string, bool) {
	if tok.HasPayload {
		return "", false
	}
	switch tok.Name {
	case ChattersToken:
		return strconv.Itoa(v.count), true
	case RandomViewerToken:
		return nextDraw(v.viewerDraws, &v.viewerDrawn), true
	}
	return nextDraw(v.draws, &v.drawn), true
}

func nextDraw(draws []string, drawn *int) string {
	if len(draws) == 0 {
		return ""
	}
	name := draws[min(*drawn, len(draws)-1)]
	*drawn++
	return name
}
