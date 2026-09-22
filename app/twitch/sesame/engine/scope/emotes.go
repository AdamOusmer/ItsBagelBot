// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"math/rand/v2"
	"strings"
)

// The tokens this scope answers. Exported for the reason the chatter scope's
// names are: the engine recognizes them in a lexed template before it builds
// the chain (it has to count the {random.emote} spans), and a name re-spelled
// on that side would be a token that silently stays literal.
//
// EmotesToken is the canonical family: {emotes:7tv}, {emotes:bttv},
// {emotes:ffz} pick the provider through the payload, the way {title:...}
// and {game:...} pick a channel. The three SevenTV/BTTV/FFZ constants below
// are the pre-simplification bare spellings, kept as silent aliases — a
// saved command keeps resolving — and emoteFamily is the one place that folds
// either spelling onto the same list.
const (
	EmotesToken        = "emotes"
	SevenTVEmotesToken = "7tvemotes"
	BTTVEmotesToken    = "bttvemotes"
	FFZEmotesToken     = "ffzemotes"
	RandomEmoteToken   = "random.emote"
)

// emoteProviders maps a {emotes:<payload>} payload to the provider constant
// it selects. Lower-cased before lookup so {emotes:7TV} is not a typo the way
// an unrecognized provider name is.
var emoteProviders = map[string]string{
	"7tv":  SevenTVEmotesToken,
	"bttv": BTTVEmotesToken,
	"ffz":  FFZEmotesToken,
}

// MaxEmoteLine caps how many BYTES of joined emote codes one span may render.
//
// Decision record. A Twitch line is 500 bytes and emitCommand splits a longer
// response across lines, so an uncapped list — 7TV's global set alone is well
// over a thousand bytes of codes — would not print a longer list, it would
// push the broadcaster's own words onto a second line and then a third. 480 is
// the {repeat:n:phrase} cap and the same number for the same reason: it leaves
// room for the few words a template puts around the list without leaving so
// much that the list stops being the point of the command.
//
// Truncation cuts between codes, never inside one: half an emote code is not
// an emote, it is a word chat renders as text, and a list ending in "Kappa
// PagM" reads as the bot being cut off rather than as a list that is long.
const MaxEmoteLine = 480

// MaxEmoteDraws bounds how many independent emotes one response may draw.
//
// Decision record. It is MaxChatterDraws' number and MaxChatterDraws' rule:
// repeated {random.emote} spans are independent draws, so a template wanting
// three emotes gets three, and past the cap the last drawn code repeats rather
// than rendering empty. A draw costs no round trip (the catalog is already in
// memory), so the cap is not about spend — it is about the shape of the
// failure, and a repeated emote reads as a template quirk while a blank reads
// as a broken bot.
const MaxEmoteDraws = 3

// EmoteSets is one snapshot of the loaded third-party emote codes, kept per
// provider because the tokens name providers.
//
// Every code in it is a single printable word that does not open with a slash:
// the fetcher filters at the boundary where the codes arrive (automod's
// sortedCodes), so this scope renders them straight into a chat line without a
// second sanitizing pass over a thousand strings on every command run.
//
// A nil list means that provider has nothing loaded, which is the same answer
// as a provider that loaded an empty set: see the Get contract.
type EmoteSets struct {
	SevenTV []string
	BTTV    []string
	FFZ     []string
}

// EmoteSource hands this scope the loaded code lists.
//
// Decision record: this reads an ALREADY-LOADED cache and never fetches. The
// codes come from the automod emote fetcher's hourly refresh — the same set
// that suppresses false positives on chat lines — so a command naming
// {7tvemotes} adds no upstream call at all. Fetching per provider inside Plan
// would put three unauthenticated third-party round trips on the command lane,
// on every run of a command any viewer can spam, which is the one thing this
// token family is not allowed to cost.
//
// What that buys is the GLOBAL sets rather than the channel's own, and the
// guides say so out loud: these are the emotes every channel has, not the ones
// a broadcaster added to theirs. Per-channel sets are three more endpoints
// keyed per broadcaster and a cache to hold them, which is a fetcher rewrite,
// not a token.
type EmoteSource interface {
	// Emotes returns the currently loaded sets. The empty answer (nothing
	// refreshed yet, a process with the refresher switched off) is what makes
	// every list render "".
	Emotes() EmoteSets
}

// Emotes answers the emote-list tokens and {random.emote} from that snapshot.
//
// It is mounted whenever a source is wired, with no module behind it: the
// refresh it reads runs for the automod gate regardless of which modules a
// broadcaster switched on, so gating the tokens on a module would make them
// stay literal for a channel whose codes are loaded and sitting in memory.
type Emotes struct {
	// Source is the loaded catalog. Never nil on a mounted scope.
	Source EmoteSource
	// Draws is how many bare {random.emote} spans the template carries,
	// counted by the engine before the chain was built. Plan needs the count
	// and cannot derive it: the chain hands each scope DISTINCT spans, and two
	// bare {random.emote} spans are one distinct span.
	Draws int
	// Pick returns a uniform index in [0,n). nil means math/rand/v2; a test
	// pins it so a drawn code is an assertion rather than a coin flip.
	Pick func(n int) int
}

// Owns claims the five names unconditionally: see the type comment. A bare
// {emotes} or one naming a provider this scope does not carry is still
// OWNED here — Get is what turns an unusable payload into a literal, the
// same split every payload-taking token in this palette uses.
func (Emotes) Owns(name string) bool {
	switch name {
	case EmotesToken, SevenTVEmotesToken, BTTVEmotesToken, FFZEmotesToken, RandomEmoteToken:
		return true
	}
	return false
}

// emoteFamily folds a span onto the provider list it names, whichever
// spelling it used: the canonical {emotes:<provider>} payload dispatch, or
// one of the three legacy bare names. ok=false covers everything this family
// does not answer and must stay literal — a bare {emotes} with no provider to
// read, a provider payload nothing here loads, and a payload on a legacy bare
// name (none of the four ever took one).
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

// Plan reads the catalog ONCE and resolves everything from that snapshot, for
// the reason the chatter scope reads its roster once: a list and a draw taken
// from two reads could disagree inside one reply ("try Kappa: KEKW LUL" where
// the drawn code is not in the list beside it), and the refresher swaps the
// catalog under both.
//
// Only the lists some span actually names are joined; the draws are picked
// only when the template carries one. A command naming none of these tokens
// never reaches Plan at all, because the chain plans a scope only for spans it
// owns.
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

// drawCodes picks this run's codes, one per bare span up to the cap. It is nil
// when the template draws none, so a response naming only {7tvemotes} builds
// no pool at all.
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

// drawPool is every loaded code across providers, concatenated.
//
// Codes are NOT de-duplicated across providers, which is deliberate: a code
// carried by both BTTV and 7TV is genuinely two loaded emotes, so it is twice
// as likely to be drawn, and folding them would cost a map of every code on
// every run that draws one — for a joke token, on the lane that answers chat.
// Within a provider the codes are already unique (the fetcher's catalog is
// sorted and compacted).
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

// emoteValues is one run's snapshot, resolved.
type emoteValues struct {
	sevenTV string
	bttv    string
	ffz     string
	draws   []string
	drawn   int
}

// fill joins the one list a span asked for. A name this scope owns but does
// not list ({random.emote}) joins nothing.
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

// Get answers all four spans. ok is true throughout for a payload-less span,
// including for a set that is EMPTY and for one that has not loaded yet.
//
// The cold-cache answer is the interesting one, and it is "" rather than the
// literal token on purpose. A literal is this palette's word for "this bot has
// no such variable", and it is permanent — but a set that has not refreshed
// yet is a state that lasts until the next tick, so a literal would print
// "{7tvemotes}" in chat for the first moments of a process and then silently
// start working. Worse, the same template would render two different SHAPES
// over its life, which reads as the bot being broken rather than as a list
// being briefly empty. Empty renders the span's fallback instead, so
// {7tvemotes|no emotes loaded} says something true either way.
//
// None of the three legacy bare names takes a payload, and {random.emote}
// never has, so a span carrying one where this palette does not expect it is
// an authoring mistake and stays literal — the same rule {chatters} and
// {time} follow. {emotes:<provider>} is the one name here where a payload is
// the point; emoteFamily is what tells the two apart.
func (v *emoteValues) Get(tok Var) (string, bool) {
	if family, ok := emoteFamily(tok); ok {
		return v.list(family), true
	}
	if tok.Name == RandomEmoteToken && !tok.HasPayload {
		return v.nextDraw(), true
	}
	return "", false
}

// list answers one resolved family's joined codes.
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

// nextDraw hands out one span's code. Past the cap it repeats the last drawn
// code rather than going empty (see MaxEmoteDraws); a catalog with nothing
// loaded renders empty, so {random.emote|🥯} reads naturally.
func (v *emoteValues) nextDraw() string {
	if len(v.draws) == 0 {
		return ""
	}
	code := v.draws[min(v.drawn, len(v.draws)-1)]
	v.drawn++
	return code
}

// joinEmoteLine renders one provider's codes as the space-separated list a
// chat line carries, stopping at MaxEmoteLine bytes on a code boundary.
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
