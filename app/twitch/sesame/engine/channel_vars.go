// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/tmpl"

	"go.uber.org/zap"
)

// The per-broadcaster module rows that gate the channel tokens. They are the
// very rows !uptime, !title and !game check — Stream Management has no master
// switch of its own, only these per-command toggles — named here rather than
// in the modules package because the engine now reads them too and one
// spelling that drifts from the other gates the wrong thing.
//
// {channel.viewers} has no row at all, and deliberately so: no built-in
// command prints a viewer count, so there is no toggle whose polarity it could
// borrow, and inventing one would put a switch on the dashboard that turns off
// exactly one variable. It mounts whenever the reader is wired.
const (
	UptimeModuleName = "uptime"
	TitleModuleName  = "title"
	GameModuleName   = "game"
)

// channelScope builds the {uptime}/{title}/{game}/{channel.viewers} scope for
// one command run, mounted per token: each of the first three is gated by the
// same row as the command that prints it, so a channel that turned !title off
// expands {game} and leaves {title} visible.
//
// It takes the lexed template because the module rows are read ONLY for the
// tokens the template actually names. A custom command mentioning none of them
// — the overwhelming majority — costs no projection read at all, where
// mounting unconditionally would add three per run to every command in every
// channel.
func (p *Pipeline) channelScope(ctx context.Context, c *module.Context, toks []tmpl.Token) (scope.Channel, bool) {
	wants := channelWantsOf(toks)
	if !wants.any() || p.streamInfo == nil {
		return scope.Channel{}, false
	}
	mounts := p.gateChannelWants(ctx, c, wants)
	ch := scope.Channel{
		Locale:   c.Locale,
		Streams:  streamLookups{p: p, c: c},
		OwnLogin: strings.ToLower(c.Env.BroadcasterUserLogin),
		Uptime:   mounts.uptime,
		Title:    mounts.title,
		Game:     mounts.game,
		Viewers:  mounts.viewers,
	}
	return ch, mounts.any()
}

// gateChannelWants narrows what the template named down to what the channel
// left switched on, so the "wanted" and "mounted" halves of the decision stay
// apart: channelScope decides shape, this decides permission.
//
// Each read is short-circuited behind its own want, which is the whole reason
// the wants are computed first — a template naming only {game} must not pay
// for the uptime and title projection reads.
//
// {channel.viewers} is passed through untouched: it has no module row (see the
// note on the module name constants above), so there is nothing to gate it on.
func (p *Pipeline) gateChannelWants(ctx context.Context, c *module.Context, w channelWants) channelWants {
	w.uptime = w.uptime && p.channelTokenOn(ctx, c, UptimeModuleName)
	w.title = w.title && p.channelTokenOn(ctx, c, TitleModuleName)
	w.game = w.game && p.channelTokenOn(ctx, c, GameModuleName)
	return w
}

// channelTokenOn reads one built-in command's toggle, the gate the command
// itself reads, so a token and its command can never disagree about whether
// the module is on.
func (p *Pipeline) channelTokenOn(ctx context.Context, c *module.Context, name string) bool {
	return p.moduleGate(c, name).BuiltinEnabled(ctx)
}

// channelWants is which channel tokens one template names.
type channelWants struct{ uptime, title, game, viewers bool }

func (w channelWants) any() bool { return w.uptime || w.title || w.game || w.viewers }

func channelWantsOf(toks []tmpl.Token) channelWants {
	var wants channelWants
	for _, tok := range toks {
		wants.mark(tok)
	}
	return wants
}

func (w *channelWants) mark(tok tmpl.Token) {
	if tok.Kind != tmpl.KindVar {
		return
	}
	switch tok.Name {
	case scope.UptimeToken:
		w.uptime = true
	case scope.TitleToken:
		w.title = true
	case scope.GameToken:
		w.game = true
	case scope.ViewersToken:
		w.viewers = true
	}
}

// streamLookups is the engine half of the channel scope: the grammar (which
// spellings resolve, how a payload folds, what offline renders as) lives in
// scope.Channel, and the cached outgress read lives here.
type streamLookups struct {
	p *Pipeline
	c *module.Context
}

// Stream reads one channel through the same cached reader !uptime calls.
//
// Every failure collapses to the zero Stream, whose UserFound is false, so all
// four tokens render their own fallback rather than a bot excuse — and rather
// than a literal, which would tell a broadcaster who spelled the token right
// that it does not exist.
func (s streamLookups) Stream(ctx context.Context, login string) scope.Stream {
	broadcasterID, named := s.address(login)
	res, err := s.p.streamInfo.Lookup(ctx, broadcasterID, named)
	if err != nil {
		s.p.log.Warn("channel token: stream lookup failed", module.BIDField(s.c.BroadcasterID), zap.Error(err))
		return scope.Stream{}
	}
	return scope.Stream{
		UserFound: res.UserFound, Live: res.Live, Title: res.Title,
		GameName: res.GameName, ViewerCount: res.ViewerCount, StartedAt: res.StartedAt,
	}
}

// address picks how the read addresses a channel: by broadcaster id for this
// channel, by login for anybody else's. The scope has already folded a span
// naming this very channel onto the empty login (see scope.Channel.OwnLogin),
// so the id path costs no Get Users hop on outgress.
func (s streamLookups) address(login string) (broadcasterID, named string) {
	if login == "" {
		return s.c.Env.BroadcasterUserID, ""
	}
	return "", login
}
