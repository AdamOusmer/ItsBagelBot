// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/i18n"
)

// The tokens this scope answers. Each takes a bare spelling for the channel
// the command ran in and, except the viewer count, a payloaded one naming
// somebody else's channel ({game:pokimane}).
//
// They are exported for the reason the viewer scope's names are: the engine
// has to recognize them in a lexed template BEFORE it reads a module row, so
// that a command mentioning none of them costs no projection read, and a name
// re-spelled on that side would be a token that silently stays literal.
//
// {channel.viewers} is dotted while the other three are bare because bare
// {channel} is already the display name in the message scope. The lexer
// lower-cases a whole name that carries no ':', so the dotted spelling is one
// key, not a name plus a payload.
const (
	UptimeToken  = "uptime"
	TitleToken   = "title"
	GameToken    = "game"
	ViewersToken = "channel.viewers"
)

// MaxChannelLogins bounds how many OTHER channels one response may look up.
//
// Decision record. Each named login is a Twitch round trip through outgress
// (Get Users to resolve it, then Get Streams), where the bare spellings are
// one lookup of a channel this bot is already in. Three is the cap for the
// same reason three is the quote and chatter-draw cap: a Twitch line is 500
// bytes, and a shoutout template that says something about each channel runs
// out of room well before a fourth — while the cost of NOT capping is paid in
// somebody else's Helix budget, on a command any viewer can run.
//
// The channel the command ran in does not count against it. It is the common
// case, it is one lookup however many of the four tokens name it, and a
// template that spends its three on other channels must still be able to say
// what its own is playing.
//
// Past the cap a span renders empty (so its fallback speaks) rather than
// staying literal: the template is not wrong, it is only asking for more than
// one chat line's worth of lookups, and a literal "{title:foo}" in chat would
// read as the bot not knowing the token at all.
const MaxChannelLogins = 3

// Stream is one channel's current session as these tokens read it.
//
// UserFound separates "Twitch has no such channel" from "that channel is
// offline", and the two render differently: an unknown channel is empty
// everywhere, while an offline one has a real title, a real category and a
// viewer count of zero. A zero-value convention could not tell them apart, and
// a template naming a misspelled login would then claim the channel exists and
// is merely dark.
type Stream struct {
	UserFound   bool
	Live        bool
	Title       string
	GameName    string
	ViewerCount int
	StartedAt   time.Time
}

// Streams reads one channel's session. The engine implements it over the same
// cached reader !uptime calls, so a chat that just asked pays no second round
// trip.
//
// An empty login means the channel the command ran in. Implementations never
// return an error: a failed read is the zero Stream (UserFound false), which
// renders every span in the family empty rather than blanking the reply.
type Streams interface {
	Stream(ctx context.Context, login string) Stream
}

// Channel answers the tokens that describe a CHANNEL rather than a viewer:
// {uptime}, {title}, {game} and {channel.viewers}.
//
// One dependency answers all four, because one Twitch read carries all four —
// so a response naming three of them costs one round trip, not three. What is
// per-token is the GATE: {uptime}, {title} and {game} are each toggled by the
// same per-broadcaster row as the built-in command that prints them (!uptime,
// !title, !game), so a channel that turned !title off must leave {title}
// visible while {game} still expands. A false flag means this scope does not
// own that name and its spans stay literal, exactly like a typo.
type Channel struct {
	// Locale is the broadcaster's language, for the shared humanizer.
	Locale string
	// Streams is the channel read. nil means nothing is wired and the scope
	// owns nothing.
	Streams Streams
	// OwnLogin is the broadcaster's own login, already lower-cased. A span
	// that names it ({title:my_own_login}) folds onto the bare spelling: one
	// read instead of two, and — the part that would be a visible bug
	// otherwise — it does not spend one of the three named-login slots on the
	// channel the command is already running in.
	OwnLogin string
	// Uptime, Title, Game and Viewers are the per-token mounts the engine
	// resolved from this broadcaster's module rows.
	Uptime  bool
	Title   bool
	Game    bool
	Viewers bool
	// Now is the clock {uptime} measures against. nil means time.Now; a test
	// pins it so a humanized span is an assertion rather than a race.
	Now func() time.Time
}

// Owns claims a name only when the read is wired and that token's own module
// row is on for this broadcaster.
func (c Channel) Owns(name string) bool {
	if c.Streams == nil {
		return false
	}
	switch name {
	case UptimeToken:
		return c.Uptime
	case TitleToken:
		return c.Title
	case GameToken:
		return c.Game
	case ViewersToken:
		return c.Viewers
	}
	return false
}

// Plan reads every channel the template names, once each, before a byte is
// rendered.
//
// The batching is the deduplication: "{title} — {game} for {uptime}" is three
// spans over ONE read, and "{title:Bob}" beside "{game:@bob}" is two spans
// over one more. It never returns an error, because a read that failed is
// already the empty answer its own span renders (see Streams).
func (c Channel) Plan(ctx context.Context, wants []Var) (Values, error) {
	out := &channelValues{
		locale: c.Locale, now: c.Now, ownLogin: c.OwnLogin,
		streams: make(map[string]Stream, len(wants)),
	}
	for _, want := range wants {
		c.planOne(ctx, out, want)
	}
	return out, nil
}

// planOne reads one span's channel unless an earlier span already did, or the
// template has spent its named-login budget.
func (c Channel) planOne(ctx context.Context, out *channelValues, want Var) {
	login, ok := out.loginOf(want)
	if !ok {
		return
	}
	if _, done := out.streams[login]; done {
		return
	}
	if !out.admit(login) {
		return
	}
	out.streams[login] = c.Streams.Stream(ctx, login)
}

// loginOf reads the channel a span addresses: its payload, or the channel the
// command ran in when it carries none — and the command's own channel again
// when the payload names it, so both spellings share one read.
//
// ok=false marks a span that addresses nothing and must stay literal: an empty
// or unusable payload ({title:}), and any payload at all on
// {channel.viewers}, which counts the channel the command ran in and has no
// use for a name. Leaving those visible shows the author their mistake instead
// of quietly answering a different question, the rule {time} and the chatter
// tokens already follow.
func (v *channelValues) loginOf(tok Var) (string, bool) {
	if !tok.HasPayload {
		return "", true
	}
	if tok.Name == ViewersToken {
		return "", false
	}
	login := normalizeLogin(tok.Payload)
	if login == "" {
		return "", false
	}
	if login == v.ownLogin {
		return "", true
	}
	return login, true
}

// channelValues is one run's resolved reads, keyed by the login each addresses
// ("" being the channel the command ran in).
type channelValues struct {
	locale   string
	ownLogin string
	now      func() time.Time
	streams  map[string]Stream
	// named counts the OTHER channels this template has already looked up, to
	// hold it under MaxChannelLogins.
	named int
}

// admit reports whether a not-yet-read channel may be looked up, charging it
// against the named-login budget. The channel the command ran in is always
// admitted and never charged (see MaxChannelLogins).
func (v *channelValues) admit(login string) bool {
	if login == "" {
		return true
	}
	if v.named >= MaxChannelLogins {
		return false
	}
	v.named++
	return true
}

// Get answers every span this scope owns. ok is true throughout for a span
// with a usable address: the read ran (or was deliberately not run, past the
// cap), and an empty result is a resolved-empty value that renders the span's
// fallback — never a literal, which would claim this bot has no such token.
func (v *channelValues) Get(tok Var) (string, bool) {
	login, ok := v.loginOf(tok)
	if !ok {
		return "", false
	}
	return v.render(tok.Name, v.streams[login]), true
}

// render turns one channel's session into one token's text. A channel Twitch
// does not know renders empty everywhere: there is no title to print and no
// zero to report about a channel that does not exist.
func (v *channelValues) render(name string, s Stream) string {
	if !s.UserFound {
		return ""
	}
	switch name {
	case TitleToken:
		return s.Title
	case GameToken:
		return s.GameName
	case ViewersToken:
		return viewerCount(s)
	}
	return v.uptime(s)
}

// viewerCount renders the audience. Offline is the pinned "0" rather than
// empty: nobody is watching an offline channel, and that is a number the
// template can say out loud, where an empty span would fire a fallback written
// for "we could not find out".
func viewerCount(s Stream) string {
	if !s.Live {
		return "0"
	}
	return strconv.Itoa(s.ViewerCount)
}

// uptime renders how long the channel has been live, through the shared
// humanizer !uptime itself prints with.
//
// An offline channel renders EMPTY, and that is the pinned decision: the
// lookup succeeded, so the span must not stay literal, but there is no
// duration to name either. Empty fires the span's fallback, which lets a
// broadcaster write "live for {uptime|not right now}" in their own voice
// instead of the bot dropping !uptime's whole offline sentence into the middle
// of theirs.
func (v *channelValues) uptime(s Stream) string {
	if !s.Live {
		return ""
	}
	return i18n.HumanizeDuration(v.locale, v.clock().Sub(s.StartedAt))
}

func (v *channelValues) clock() time.Time {
	if v.now != nil {
		return v.now()
	}
	return time.Now()
}
