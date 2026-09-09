// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"math/rand/v2"
	"strconv"
)

// The tokens this scope answers. Exported for the reason the module scope's
// names are: the engine recognizes them in a lexed template before it builds
// the chain (it has to count the {random.chatter} spans), and a name
// re-spelled on that side would be a token that silently stays literal.
const (
	ChattersToken      = "chatters"
	RandomChatterToken = "random.chatter"
)

// MaxChatterDraws bounds how many independent names one response may draw.
//
// Decision record. Repeated {random.chatter} spans are independent draws, the
// same rule {quote} follows, because a template naming two people should name
// two. Three is the cap for the reason three is the quote cap: a Twitch line
// is 500 bytes and a template that says something ABOUT each drawn name runs
// out of room well before a fourth. Unlike a quote a draw costs no round trip,
// so the cap is not about spend — it is about the shape of the failure: past
// the cap the last drawn name repeats rather than rendering empty, and a
// repeated name reads as a template mistake while a blank reads as a broken
// bot.
const MaxChatterDraws = 3

// Chatter is one remembered viewer as this scope renders them: the name chat
// reads, already sanitized by the engine (roster names come off the chat wire,
// so they are viewer-supplied like {touser} is), plus the numeric Twitch id
// the draw excludes on.
//
// The id is here to be COMPARED, never to be printed: the palette prints the
// sender's id through {userid} and nobody else's, and a token that leaked one
// would put an identifier in chat that the person it names never chose to show.
type Chatter struct {
	ID   uint64
	Name string
}

// Roster is the chatter set this replica has watched speak in one channel.
//
// Decision record: this reads an in-memory roster and never Helix, which is
// the whole reason the two tokens can exist at all. Helix's get-chatters is a
// per-channel authorized call with its own rate budget, and a token is
// resolved once per command run — a popular channel's !lurk would spend the
// channel's Helix budget answering a joke command, and a token that has to
// wait on a network round trip cannot be planned beside the rest of a reply
// without slowing every one of them. The roster is already fed by every chat
// line the pipeline observes (engine/roster.go), so the read is a map copy
// under a read lock.
//
// What that buys is a different meaning, and the guides say so out loud: this
// is who has SPOKEN recently, not who has the channel open. The roster keeps
// up to a few thousand identities per channel and evicts arbitrarily past
// that, so a lurker who has never typed is not in it and a chatter who spoke
// long enough ago may have been evicted. "Recently active chatters" is the
// honest wording, and it is the wording the token is documented under.
type Roster interface {
	// Chatters returns this channel's remembered viewers. Order is
	// unspecified; the empty answer (no roster, an unknown channel, a replica
	// that has seen nothing yet) is what makes {chatters} render "0".
	Chatters() []Chatter
}

// Chatters answers {chatters} and {random.chatter} from that roster.
//
// It is mounted unconditionally — there is no opt-in module behind it, and the
// roster is fed by the same chat lines that trigger the command — so neither
// token ever stays literal for a broadcaster who spelled it right. An empty
// roster is a real answer ("0", and an empty draw), not a missing scope.
type Chatters struct {
	Roster Roster
	// Exclude are the Twitch ids {random.chatter} never draws: the bot itself
	// and the broadcaster.
	//
	// Ids rather than logins, and that is deliberate. The engine knows the bot
	// as a numeric id (it is what the pipeline already filters its own echo
	// on) and has no login for it at all, while a login is renameable and the
	// roster's key is whatever login the viewer carried when they last spoke —
	// so a login comparison would leak the broadcaster back into the draw the
	// day they renamed. An id nobody knows is zero, and the roster never
	// stores a zero id (roster.go rosterKey), so an unknown exclusion excludes
	// nothing rather than everything.
	Exclude []uint64
	// Draws is how many bare {random.chatter} spans the template carries,
	// counted by the engine before the chain was built. Plan needs the count
	// and cannot derive it: the chain hands each scope DISTINCT spans, and two
	// bare {random.chatter} spans are one distinct span.
	Draws int
	// Pick returns a uniform index in [0,n). nil means math/rand/v2; a test
	// pins it so a drawn name is an assertion rather than a coin flip.
	Pick func(n int) int
}

// Owns claims both names unconditionally: see the type comment.
func (Chatters) Owns(name string) bool {
	return name == ChattersToken || name == RandomChatterToken
}

// Plan reads the roster ONCE and resolves everything from that snapshot.
//
// One read even when only {chatters} names the scope, and even though a count
// alone could be taken more cheaply: a count read separately from the list
// could disagree with the name drawn beside it in the same reply ("3 chatters,
// say hi to sam" where sam is no longer one of the three), and two reads of a
// map that every chat line writes is exactly how that happens.
func (c Chatters) Plan(_ context.Context, _ []Var) (Values, error) {
	roster := c.snapshot()
	return &chatterValues{count: len(roster), draws: c.drawNames(roster)}, nil
}

func (c Chatters) snapshot() []Chatter {
	if c.Roster == nil {
		return nil
	}
	return c.Roster.Chatters()
}

// drawNames picks this run's names, one per bare span up to the cap. It is nil
// when the template draws none, so a response naming only {chatters} does no
// picking at all.
func (c Chatters) drawNames(roster []Chatter) []string {
	pool := c.pickable(roster)
	if len(pool) == 0 {
		return nil
	}
	wanted := min(c.Draws, MaxChatterDraws)
	draws := make([]string, 0, wanted)
	for i := 0; i < wanted; i++ {
		draws = append(draws, pool[c.pick(len(pool))])
	}
	return draws
}

// pickable is the roster minus the excluded ids and minus anyone the engine
// could not name. Draws are independent, so the pool is not consumed: two
// spans may name the same person, exactly as two {random} spans may roll the
// same number.
func (c Chatters) pickable(roster []Chatter) []string {
	pool := make([]string, 0, len(roster))
	for _, who := range roster {
		if who.Name == "" || c.excluded(who.ID) {
			continue
		}
		pool = append(pool, who.Name)
	}
	return pool
}

func (c Chatters) excluded(id uint64) bool {
	for _, skip := range c.Exclude {
		if id == skip {
			return true
		}
	}
	return false
}

func (c Chatters) pick(n int) int {
	if c.Pick != nil {
		return c.Pick(n)
	}
	return rand.IntN(n)
}

// chatterValues is one run's snapshot, resolved.
type chatterValues struct {
	count int
	draws []string
	drawn int
}

// Get answers both spans. ok is true throughout for a payload-less span: the
// roster read ran, so an empty channel renders "0" and an empty draw renders
// the span's fallback rather than the literal token, which would claim the bot
// has no such variable.
//
// Neither token takes a payload, so a span carrying one is an authoring
// mistake and stays literal — which shows the author their typo instead of
// quietly ignoring what they wrote, the same rule {time} and the {song} family
// follow.
func (v *chatterValues) Get(tok Var) (string, bool) {
	if tok.HasPayload {
		return "", false
	}
	if tok.Name == ChattersToken {
		return strconv.Itoa(v.count), true
	}
	return v.nextDraw(), true
}

// nextDraw hands out one span's name. Past the cap it repeats the last drawn
// name rather than going empty (see MaxChatterDraws); a channel with nobody
// left to draw renders empty, so {random.chatter|someone} reads naturally.
func (v *chatterValues) nextDraw() string {
	if len(v.draws) == 0 {
		return ""
	}
	name := v.draws[min(v.drawn, len(v.draws)-1)]
	v.drawn++
	return name
}
