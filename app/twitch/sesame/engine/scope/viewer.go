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

const (
	FollowageToken        = "followage"
	AccountAgeToken       = "accountage"
	PointsToken           = "points"
	PointsNameToken       = "points.name"
	PointsNameLegacyToken = "pointsname"
	WatchTimeToken        = "watchtime"
)

type Spans interface {
	Span(ctx context.Context, login string) string
}

type Balance struct {
	Points       int64
	WatchSeconds uint64
	Found        bool
}

type Balances interface {
	Balance(ctx context.Context, login string) Balance
	CurrencyName(ctx context.Context) string
}

type Viewer struct {
	Locale   string
	Sender   string
	Follow   Spans
	Account  Spans
	Balances Balances
}

func (v Viewer) Owns(tok Var) bool {
	switch tok.Name {
	case FollowageToken:
		return v.Follow != nil
	case AccountAgeToken:
		return v.Account != nil
	case PointsToken, WatchTimeToken, PointsNameToken, PointsNameLegacyToken:
		return v.Balances != nil
	}
	return false
}

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

type viewerRef struct {
	family string
	login  string
}

func refOf(tok Var, sender string) (viewerRef, bool) {
	if tok.Name == PointsNameToken || tok.Name == PointsNameLegacyToken {
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

func familyOf(name string) string {
	if name == WatchTimeToken {
		return PointsToken
	}
	return name
}

func normalizeLogin(payload string) string {
	login := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(payload), "@"))
	if strings.ContainsFunc(login, func(r rune) bool { return r < ' ' || r == '\x7f' }) {
		return ""
	}
	return login
}

type viewerValues struct {
	sender       string
	locale       string
	spans        map[viewerRef]string
	balances     map[string]Balance
	currency     string
	currencyDone bool
}

func (o *viewerValues) planCurrency(ctx context.Context, balances Balances) {
	if o.currencyDone {
		return
	}
	o.currencyDone, o.currency = true, balances.CurrencyName(ctx)
}

func (o *viewerValues) Get(tok Var) (string, bool) {
	ref, ok := refOf(tok, o.sender)
	if !ok {
		return "", false
	}
	switch tok.Name {
	case PointsNameToken, PointsNameLegacyToken:
		return o.currency, true
	case PointsToken:
		return o.points(ref.login), true
	case WatchTimeToken:
		return o.watchTime(ref.login), true
	}
	return o.spans[ref], true
}

func (o *viewerValues) points(login string) string {
	bal := o.balances[login]
	if !bal.Found {
		return ""
	}
	return strconv.FormatInt(bal.Points, 10)
}

func (o *viewerValues) watchTime(login string) string {
	bal := o.balances[login]
	if !bal.Found {
		return ""
	}
	return i18n.HumanizeDuration(o.locale, time.Duration(bal.WatchSeconds)*time.Second)
}
