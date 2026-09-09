// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/pkg/tmpl"

	"go.uber.org/zap"
)

// The per-broadcaster module rows that gate the viewer tokens. They are the
// same rows !followage and !accountage check, named here rather than in the
// modules package because the engine now reads them too and one spelling that
// drifts from the other gates the wrong thing.
const (
	FollowageModuleName  = "followage"
	AccountAgeModuleName = "accountage"
)

// viewerScope builds the {followage}/{accountage}/{points} scope for one
// command run, mounted per family: each family is gated by its own module, so
// a channel with Loyalty on and Followage off expands {points} and leaves
// {followage} visible.
//
// It takes the lexed template because the module rows are read ONLY for the
// families the template actually names. A custom command mentioning none of
// these tokens — the overwhelming majority — costs no projection read at all,
// where mounting unconditionally would add three per run to every command in
// every channel.
func (p *Pipeline) viewerScope(ctx context.Context, c *module.Context, toks []tmpl.Token) (scope.Viewer, bool) {
	wants := viewerWantsOf(toks)
	if !wants.any() {
		return scope.Viewer{}, false
	}
	v := scope.Viewer{
		Locale:   c.Locale,
		Sender:   strings.ToLower(c.Env.ChatterUserLogin),
		Follow:   p.followSpans(ctx, c, wants.follow),
		Account:  p.accountSpans(ctx, c, wants.account),
		Balances: p.viewerBalances(ctx, c, wants.loyalty),
	}
	return v, v.Follow != nil || v.Account != nil || v.Balances != nil
}

// viewerWants is which viewer families one template names.
type viewerWants struct{ follow, account, loyalty bool }

func (w viewerWants) any() bool { return w.follow || w.account || w.loyalty }

func viewerWantsOf(toks []tmpl.Token) viewerWants {
	var wants viewerWants
	for _, tok := range toks {
		wants.mark(tok)
	}
	return wants
}

func (w *viewerWants) mark(tok tmpl.Token) {
	if tok.Kind != tmpl.KindVar {
		return
	}
	switch tok.Name {
	case scope.FollowageToken:
		w.follow = true
	case scope.AccountAgeToken:
		w.account = true
	case scope.PointsToken, scope.PointsNameToken, scope.WatchTimeToken:
		w.loyalty = true
	}
}

// followSpans mounts {followage} when the reader is wired and the channel's
// Followage module is on. It reuses the built-in command's own gate, so the
// token and !followage can never disagree about whether the module is on.
func (p *Pipeline) followSpans(ctx context.Context, c *module.Context, wanted bool) scope.Spans {
	if !p.spanFamilyOn(ctx, c, spanFamily{wanted: wanted, wired: p.followage != nil, module: FollowageModuleName}) {
		return nil
	}
	return viewerSpans{p: p, c: c, logMsg: "followage token: lookup failed", moment: p.followedAt(c)}
}

// accountSpans mounts {accountage} under the Account-age module's own gate.
func (p *Pipeline) accountSpans(ctx context.Context, c *module.Context, wanted bool) scope.Spans {
	if !p.spanFamilyOn(ctx, c, spanFamily{wanted: wanted, wired: p.accountAge != nil, module: AccountAgeModuleName}) {
		return nil
	}
	return viewerSpans{p: p, c: c, logMsg: "accountage token: lookup failed", moment: p.accountCreatedAt(c)}
}

// spanFamily is what has to be true before a duration token family mounts:
// the template names it, the reader behind it is wired, and the channel's
// module row for it is on.
type spanFamily struct {
	wanted bool
	wired  bool
	module string
}

// spanFamilyOn answers that gate. The two families share it rather than
// spelling it out each, so a third one cannot arrive with two of the three
// checks and mount in a channel that has the module switched off.
func (p *Pipeline) spanFamilyOn(ctx context.Context, c *module.Context, f spanFamily) bool {
	if !f.wanted || !f.wired {
		return false
	}
	return ModuleGate{Proj: p.proj, Log: p.log, BroadcasterID: c.BroadcasterID, Name: f.module}.BuiltinEnabled(ctx)
}

// viewerBalances mounts {points}/{watchtime}/{pointsname} when loyalty is
// wired and the module is on for this channel.
//
// The config is read ONCE, here, and the currency name carried on the
// resolver: it decides both whether the family mounts at all and what
// {pointsname} renders, and reading it twice would let one response print a
// currency name from a module the rest of it treated as off.
func (p *Pipeline) viewerBalances(ctx context.Context, c *module.Context, wanted bool) scope.Balances {
	if !wanted || p.loyalty == nil {
		return nil
	}
	cfg, on := ReadLoyaltyConfig(ctx, p.proj, c.BroadcasterID)
	if !on {
		return nil
	}
	return loyaltyBalances{p: p, c: c, currency: cfg.Name()}
}

// viewerSpans answers one duration token ({followage:...}, {accountage:...})
// through the cached lookup the matching built-in command runs, one round
// trip per distinct viewer per response.
//
// The two families are one type rather than two because they differ only in
// which lookup answers the moment: the collapse below is a rule about the
// TOKEN, not about either lookup, and while it lived twice the two copies
// were free to drift into answering an unresolvable span differently.
type viewerSpans struct {
	p      *Pipeline
	c      *module.Context
	logMsg string
	moment func(ctx context.Context, login string) (time.Time, bool, error)
}

// Span renders how long ago this viewer's moment was.
//
// Every "we cannot say" outcome collapses to the empty string, which renders
// the span's fallback: an unknown user, a viewer who does not follow, and the
// broadcaster themselves (who cannot follow their own channel, so the lookup
// answers not-following) all read as the broadcaster's own fallback phrase
// rather than as !followage's sentence, which would be a whole sentence
// dropped into the middle of theirs.
func (s viewerSpans) Span(ctx context.Context, login string) string {
	at, found, err := s.moment(ctx, login)
	if err != nil {
		s.p.log.Warn(s.logMsg, module.BIDField(s.c.BroadcasterID), zap.Error(err))
		return ""
	}
	if !found {
		return ""
	}
	return i18n.HumanizeDuration(s.c.Locale, time.Since(at))
}

// followedAt answers when this viewer's follow started, and false for every
// outcome {followage} has to render its fallback for: an unknown user, and a
// viewer who does not follow (the broadcaster included, who cannot follow
// their own channel).
func (p *Pipeline) followedAt(c *module.Context) func(context.Context, string) (time.Time, bool, error) {
	return func(ctx context.Context, login string) (time.Time, bool, error) {
		res, err := p.followage.Lookup(ctx, c.Env.BroadcasterUserID, viewerIDFor(c, login), login)
		return res.FollowedAt, res.UserFound && res.Following, err
	}
}

// accountCreatedAt answers when this viewer's account was made, and false for
// a user the lookup does not know.
func (p *Pipeline) accountCreatedAt(c *module.Context) func(context.Context, string) (time.Time, bool, error) {
	return func(ctx context.Context, login string) (time.Time, bool, error) {
		res, err := p.accountAge.Lookup(ctx, viewerIDFor(c, login), login)
		return res.CreatedAt, res.UserFound, err
	}
}

// viewerIDFor supplies the platform id that lets a lookup skip resolving a
// login: the chatter's own id when the span addresses the chatter, and
// nothing otherwise — exactly what parseLookupTarget hands !followage, where
// an explicit "@name" argument carries a login and no id.
func viewerIDFor(c *module.Context, login string) string {
	if login == strings.ToLower(c.Env.ChatterUserLogin) {
		return c.Env.ChatterUserID
	}
	return ""
}

// loyaltyBalances answers {points}, {watchtime} and {pointsname} through the
// same store and the same module config !points reads.
type loyaltyBalances struct {
	p        *Pipeline
	c        *module.Context
	currency string
}

// CurrencyName is resolved at mount, so {pointsname} costs no read of its own
// and cannot disagree with the gate that mounted it.
func (b loyaltyBalances) CurrencyName(context.Context) string { return b.currency }

// Balance reads one viewer's standing. Nothing here grants, spends or bumps:
// the token family is read-only by construction, and granting stays on
// "!points give" and the dashboard.
func (b loyaltyBalances) Balance(ctx context.Context, login string) scope.Balance {
	viewerID, ok := b.viewerID(login)
	if !ok {
		return scope.Balance{}
	}
	bal, err := b.p.loyalty.BalanceGet(ctx, b.c.BroadcasterID, viewerID)
	if err != nil {
		b.p.log.Warn("points token: balance read failed", module.BIDField(b.c.BroadcasterID), zap.Error(err))
		return scope.Balance{}
	}
	return scope.Balance{Points: bal.Points, WatchSeconds: bal.WatchSeconds, Found: true}
}

// viewerID resolves the login a span addresses to the id the balance store is
// keyed on: the chatter's own id when it is the chatter (the path !points
// takes), otherwise the roster of viewers this replica has seen speak — the
// same path a target-addressed counter ({counter:target:…}) keys its bump on.
//
// A login nobody has spoken where this replica could see it resolves to
// nothing, and the span renders empty. It deliberately does NOT fall back to
// the sender the way {counter:target:…} does: a bump landing on the wrong
// bucket is invisible, while "{points:ferret_king}" printing the SENDER's
// balance under ferret_king's name is a number chat would read and believe.
func (b loyaltyBalances) viewerID(login string) (uint64, bool) {
	if login == strings.ToLower(b.c.Env.ChatterUserLogin) {
		id, err := strconv.ParseUint(b.c.Env.ChatterUserID, 10, 64)
		return id, err == nil && id != 0
	}
	viewer, found := b.p.roster.Resolve(b.c.BroadcasterID, login)
	return viewer.ID, found && viewer.ID != 0
}
