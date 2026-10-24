// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"math/rand/v2"
	"slices"
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

type RosterView interface {
	Len() int
	At(i int) Chatter
	Slot(id uint64) (int, bool)
}

type Roster interface {
	View(read func(RosterView))
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
	out := &chatterValues{}
	if c.Roster != nil {
		c.Roster.View(func(v RosterView) {
			out.count = v.Len()
			out.draws = c.drawRoster(v)
		})
	}
	if c.ViewerDraws > 0 && c.Viewers != nil {
		if viewers, ok := c.Viewers.Viewers(ctx); ok {
			out.viewerDraws = c.drawFrom(chatterDrawPool(viewers, c.ViewerExclude), c.ViewerDraws)
		}
	}
	return out, nil
}

// drawRoster maps each draw over the drawable count onto a roster slot by
// skipping the excluded slots, so it never builds the pool. An unnamed slot
// falls back to the pool path, which keeps the draw uniform over named chatters.
func (c Chatters) drawRoster(v RosterView) []string {
	skip := excludedSlots(v, c.Exclude)
	size := v.Len() - len(skip)
	if size <= 0 || c.Draws <= 0 {
		return nil
	}
	wanted := min(c.Draws, MaxChatterDraws)
	draws := make([]string, 0, wanted)
	for range wanted {
		who := v.At(slotAfterSkips(c.pick(size), skip))
		if who.Name == "" {
			return c.drawFrom(chatterDrawPool(rosterEntries(v), c.Exclude), c.Draws)
		}
		draws = append(draws, who.Name)
	}
	return draws
}

func excludedSlots(v RosterView, exclude []uint64) []int {
	skip := make([]int, 0, len(exclude))
	for _, id := range exclude {
		if slot, ok := v.Slot(id); ok && !slices.Contains(skip, slot) {
			skip = append(skip, slot)
		}
	}
	slices.Sort(skip)
	return skip
}

func slotAfterSkips(k int, sortedSkip []int) int {
	for _, s := range sortedSkip {
		if s > k {
			break
		}
		k++
	}
	return k
}

func rosterEntries(v RosterView) []Chatter {
	out := make([]Chatter, v.Len())
	for i := range out {
		out[i] = v.At(i)
	}
	return out
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
