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

const (
	FollowageModuleName  = "followage"
	AccountAgeModuleName = "accountage"
)

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
	case scope.PointsToken, scope.PointsNameToken, scope.PointsNameLegacyToken, scope.WatchTimeToken:
		w.loyalty = true
	}
}

func (p *Pipeline) followSpans(ctx context.Context, c *module.Context, wanted bool) scope.Spans {
	if !p.spanFamilyOn(ctx, c, spanFamily{wanted: wanted, wired: p.followage != nil, module: FollowageModuleName}) {
		return nil
	}
	return viewerSpans{p: p, c: c, logMsg: "followage token: lookup failed", moment: p.followedAt(c)}
}

func (p *Pipeline) accountSpans(ctx context.Context, c *module.Context, wanted bool) scope.Spans {
	if !p.spanFamilyOn(ctx, c, spanFamily{wanted: wanted, wired: p.accountAge != nil, module: AccountAgeModuleName}) {
		return nil
	}
	return viewerSpans{p: p, c: c, logMsg: "accountage token: lookup failed", moment: p.accountCreatedAt(c)}
}

type spanFamily struct {
	wanted bool
	wired  bool
	module string
}

func (p *Pipeline) spanFamilyOn(ctx context.Context, c *module.Context, f spanFamily) bool {
	if !f.wanted || !f.wired {
		return false
	}
	return ModuleGate{Proj: p.proj, Log: p.log, BroadcasterID: c.BroadcasterID, Name: f.module}.BuiltinEnabled(ctx)
}

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

type viewerSpans struct {
	p      *Pipeline
	c      *module.Context
	logMsg string
	moment func(ctx context.Context, login string) (time.Time, bool, error)
}

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

func (p *Pipeline) followedAt(c *module.Context) func(context.Context, string) (time.Time, bool, error) {
	return func(ctx context.Context, login string) (time.Time, bool, error) {
		res, err := p.followage.Lookup(ctx, c.Env.BroadcasterUserID, viewerIDFor(c, login), login)
		return res.FollowedAt, res.UserFound && res.Following, err
	}
}

func (p *Pipeline) accountCreatedAt(c *module.Context) func(context.Context, string) (time.Time, bool, error) {
	return func(ctx context.Context, login string) (time.Time, bool, error) {
		res, err := p.accountAge.Lookup(ctx, viewerIDFor(c, login), login)
		return res.CreatedAt, res.UserFound, err
	}
}

func viewerIDFor(c *module.Context, login string) string {
	if login == strings.ToLower(c.Env.ChatterUserLogin) {
		return c.Env.ChatterUserID
	}
	return ""
}

type loyaltyBalances struct {
	p        *Pipeline
	c        *module.Context
	currency string
}

func (b loyaltyBalances) CurrencyName(context.Context) string { return b.currency }

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

func (b loyaltyBalances) viewerID(login string) (uint64, bool) {
	if login == strings.ToLower(b.c.Env.ChatterUserLogin) {
		id, err := strconv.ParseUint(b.c.Env.ChatterUserID, 10, 64)
		return id, err == nil && id != 0
	}
	viewer, found := b.p.roster.Resolve(b.c.BroadcasterID, login)
	return viewer.ID, found && viewer.ID != 0
}
