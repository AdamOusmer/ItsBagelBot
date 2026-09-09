// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/i18n"
)

// The tokens this scope answers, in the two spellings each accepts: bare
// ({followage}) for whoever ran the command, and payloaded
// ({followage:ferret_king}) for a named viewer.
//
// They are exported because this scope is mounted PER FAMILY — each family is
// gated by its own broadcaster module — and the engine has to recognize the
// names in a lexed template before it reads any module row. Re-spelling them
// on that side would be two lists free to drift, and a drifted name is a
// token that silently stays literal for every broadcaster.
const (
	FollowageToken  = "followage"
	AccountAgeToken = "accountage"
	PointsToken     = "points"
	PointsNameToken = "pointsname"
	WatchTimeToken  = "watchtime"
)

// Spans is one viewer-duration lookup: how long a login has followed this
// channel, or how long their account has existed. The engine implements it
// over the same cached readers !followage and !accountage already call, so a
// channel whose chat just asked pays no second round trip.
//
// The empty string means "the lookup ran and produced nothing": not
// following, no such user, or a read that failed. The span then renders its
// fallback, which is a broadcaster-authored phrase rather than a bot excuse.
// Module-off is deliberately NOT expressible here — an off module leaves the
// scope's field nil, the scope does not own the token, and the span stays
// literal exactly like a typo.
type Spans interface {
	Span(ctx context.Context, login string) string
}

// Balance is one viewer's loyalty standing, in the raw units the store keeps
// so the rendering (a plain integer, a humanized span) stays in this package
// beside every other token's.
type Balance struct {
	Points       int64
	WatchSeconds uint64
	// Found is false when the login could not be resolved to a viewer this
	// channel has seen, or the read failed. It is a separate field rather
	// than a zero-value convention because a real, earned zero is a normal
	// answer: printing "0" for a viewer nobody could look up would read as
	// "they have none" instead of "we do not know them".
	Found bool
}

// Balances reads loyalty standings plus the channel's currency name. The
// engine implements it over the same store and the same module config
// !points reads.
type Balances interface {
	Balance(ctx context.Context, login string) Balance
	CurrencyName(ctx context.Context) string
}

// Viewer answers the tokens that describe ONE viewer through a lookup the
// built-in commands already own: {followage}, {accountage}, {points},
// {watchtime} and the channel's {pointsname}.
//
// Each dependency is mounted on its own, because each is gated by its own
// per-broadcaster module: a channel with Loyalty on and Followage off must
// expand {points} and leave {followage} visible. A nil field means the family
// is not mounted, Owns declines its names, and its spans stay literal.
type Viewer struct {
	// Locale is the broadcaster's language, for the shared humanizer.
	Locale string
	// Sender is the chatter's login, already lower-cased: the viewer a bare
	// {followage} / {points} addresses.
	Sender string
	// Follow and Account are the two duration lookups, gated by the
	// "followage" and "accountage" modules respectively.
	Follow  Spans
	Account Spans
	// Balances is the loyalty read, gated by the "loyalty" module.
	Balances Balances
}

// Owns claims a token only when the dependency that answers it is mounted.
func (v Viewer) Owns(name string) bool {
	switch name {
	case FollowageToken:
		return v.Follow != nil
	case AccountAgeToken:
		return v.Account != nil
	case PointsToken, WatchTimeToken, PointsNameToken:
		return v.Balances != nil
	}
	return false
}

// Plan runs every lookup the template needs, once per (family, login), before
// a single byte is rendered.
//
// The batching is the deduplication: the chain hands over distinct SPANS, and
// "{points} you have, {watchtime} watched" is two spans over one balance
// read, while "{followage:@Bob}" and "{followage:bob}" are two spans over one
// follow lookup. A response naming a viewer five ways therefore costs one
// round trip per family, not five.
//
// It never returns an error: a failed lookup is the family's own empty answer
// (see Spans), not a reason to blank the four tokens beside it, which is what
// a scope-level error would do.
func (v Viewer) Plan(ctx context.Context, wants []Var) (Values, error) {
	out := &viewerValues{
		sender:   v.Sender,
		locale:   v.Locale,
		spans:    make(map[viewerRef]string, len(wants)),
		balances: make(map[string]Balance, len(wants)),
	}
	for _, want := range wants {
		v.planOne(ctx, out, want)
	}
	return out, nil
}

// planOne resolves one span's lookup unless an earlier span already did.
func (v Viewer) planOne(ctx context.Context, out *viewerValues, want Var) {
	ref, ok := refOf(want, v.Sender)
	if !ok {
		return
	}
	switch ref.family {
	case FollowageToken:
		planSpan(ctx, out, ref, v.Follow)
	case AccountAgeToken:
		planSpan(ctx, out, ref, v.Account)
	case PointsToken:
		v.planBalance(ctx, out, ref.login)
	case PointsNameToken:
		out.planCurrency(ctx, v.Balances)
	}
}

func planSpan(ctx context.Context, out *viewerValues, ref viewerRef, spans Spans) {
	if _, done := out.spans[ref]; done {
		return
	}
	out.spans[ref] = spans.Span(ctx, ref.login)
}

func (v Viewer) planBalance(ctx context.Context, out *viewerValues, login string) {
	if _, done := out.balances[login]; done {
		return
	}
	out.balances[login] = v.Balances.Balance(ctx, login)
}

// viewerRef is one span reduced to what actually decides its lookup: the
// family that answers it and the login it addresses.
type viewerRef struct {
	family string
	login  string
}

// refOf reads the viewer a span addresses — its payload, or the sender when
// it carries none.
//
// ok=false marks a span that addresses nobody and must stay literal:
// {points:} names an empty login, and {pointsname:anything} is the channel's
// currency name given a viewer it has no use for, which is an authoring
// mistake worth leaving visible rather than silently ignoring.
func refOf(tok Var, sender string) (viewerRef, bool) {
	if tok.Name == PointsNameToken {
		return viewerRef{family: PointsNameToken}, !tok.HasPayload
	}
	login := sender
	if tok.HasPayload {
		login = normalizeLogin(tok.Payload)
	}
	if login == "" {
		return viewerRef{}, false
	}
	return viewerRef{family: familyOf(tok.Name), login: login}, true
}

// familyOf groups the names that share ONE lookup. {points} and {watchtime}
// are two fields of a single balance read, so a response printing both for
// one viewer must not fan out twice.
func familyOf(name string) string {
	if name == WatchTimeToken {
		return PointsToken
	}
	return name
}

// normalizeLogin folds a payload login the way the engine folds the argument
// a command was given: trim, drop the '@' a chatter habitually types, and
// lower-case, so {points:@Bob} and {points:bob} are one viewer and one read.
//
// A payload carrying a control character resolves to no login at all rather
// than being scrubbed. This value is only ever a lookup KEY — it is never
// rendered, so nothing here can inject a slash-verb the way a viewer-supplied
// {args} could — but a login with an embedded newline is not a login anybody
// meant, and leaving that span literal shows the author the typo instead of
// quietly answering for somebody else.
func normalizeLogin(payload string) string {
	login := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(payload), "@"))
	if strings.ContainsFunc(login, func(r rune) bool { return r < ' ' || r == '\x7f' }) {
		return ""
	}
	return login
}

// viewerValues is one run's resolved lookups. Get re-derives each span's ref
// rather than keying on the raw span, so two spellings of one viewer read the
// single answer their single lookup produced.
type viewerValues struct {
	sender   string
	locale   string
	spans    map[viewerRef]string
	balances map[string]Balance
	currency string
	// currencyDone separates "not asked for" from "the channel configured an
	// empty name", which the currency string alone cannot.
	currencyDone bool
}

func (o *viewerValues) planCurrency(ctx context.Context, balances Balances) {
	if o.currencyDone {
		return
	}
	o.currencyDone, o.currency = true, balances.CurrencyName(ctx)
}

// Get answers every span this scope planned. ok is true throughout for a span
// with a usable ref: the lookup ran, and an empty result is a resolved-empty
// value that renders the span's fallback — never a literal, which would say
// "this bot has no such token" about a token it does have.
func (o *viewerValues) Get(tok Var) (string, bool) {
	ref, ok := refOf(tok, o.sender)
	if !ok {
		return "", false
	}
	switch tok.Name {
	case PointsNameToken:
		return o.currency, true
	case PointsToken:
		return o.points(ref.login), true
	case WatchTimeToken:
		return o.watchTime(ref.login), true
	}
	return o.spans[ref], true
}

// points renders a standing. A viewer this channel could not resolve renders
// empty (so the span's fallback speaks) rather than "0", which would read as
// an earned zero.
func (o *viewerValues) points(login string) string {
	bal := o.balances[login]
	if !bal.Found {
		return ""
	}
	return strconv.FormatInt(bal.Points, 10)
}

// watchTime renders accrued watch time through the shared humanizer — the
// same wording !uptime, !followage and {countdown} print — so every span the
// bot names reads alike.
func (o *viewerValues) watchTime(login string) string {
	bal := o.balances[login]
	if !bal.Found {
		return ""
	}
	return i18n.HumanizeDuration(o.locale, time.Duration(bal.WatchSeconds)*time.Second)
}
