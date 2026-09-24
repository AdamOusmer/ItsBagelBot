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

const (
	UptimeModuleName = "uptime"
	TitleModuleName  = "title"
	GameModuleName   = "game"
)

func (p *Pipeline) channelScope(ctx context.Context, c *module.Context, toks []tmpl.Token) (scope.Channel, bool) {
	wants := channelWantsOf(toks)
	if !wants.any() {
		return scope.Channel{}, false
	}
	streamsWired := p.streamInfo != nil
	var mounts channelWants
	if streamsWired {
		mounts = p.gateChannelWants(ctx, c, wants)
	}
	ch := scope.Channel{
		Locale:   c.Locale,
		OwnLogin: strings.ToLower(c.Env.BroadcasterUserLogin),
		Uptime:   mounts.uptime,
		Title:    mounts.title,
		Game:     mounts.game,
		Viewers:  mounts.viewers,
	}
	if streamsWired {
		ch.Streams = streamLookups{p: p, c: c}
	}
	if wants.counts && p.channelCounts != nil {
		ch.Counts = channelCountsLookup{p: p, c: c}
	}
	return ch, (ch.Streams != nil && mounts.any()) || ch.Counts != nil
}

func (p *Pipeline) gateChannelWants(ctx context.Context, c *module.Context, w channelWants) channelWants {
	w.uptime = w.uptime && p.channelTokenOn(ctx, c, UptimeModuleName)
	w.title = w.title && p.channelTokenOn(ctx, c, TitleModuleName)
	w.game = w.game && p.channelTokenOn(ctx, c, GameModuleName)
	return w
}

func (p *Pipeline) channelTokenOn(ctx context.Context, c *module.Context, name string) bool {
	return p.moduleGate(c, name).BuiltinEnabled(ctx)
}

type channelWants struct{ uptime, title, game, viewers, counts bool }

func (w channelWants) any() bool { return w.uptime || w.title || w.game || w.viewers || w.counts }

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
	case scope.FollowersToken, scope.SubsToken:
		w.counts = true
	}
}

type streamLookups struct {
	p *Pipeline
	c *module.Context
}

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

func (s streamLookups) address(login string) (broadcasterID, named string) {
	if login == "" {
		return s.c.Env.BroadcasterUserID, ""
	}
	return "", login
}

type channelCountsLookup struct {
	p *Pipeline
	c *module.Context
}

func (l channelCountsLookup) Counts(ctx context.Context) scope.ChannelCountsResult {
	res, err := l.p.channelCounts.Lookup(ctx, l.c.Env.BroadcasterUserID)
	if err != nil {
		l.p.log.Warn("channel token: counts lookup failed", module.BIDField(l.c.BroadcasterID), zap.Error(err))
		return scope.ChannelCountsResult{}
	}
	return scope.ChannelCountsResult{
		Followers: res.Followers, FollowersOK: res.FollowersOK,
		Subs: res.Subs, SubsOK: res.SubsOK,
	}
}
