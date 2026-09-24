// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/tmpl"
)

func (p *Pipeline) chattersScope(c *module.Context, toks []tmpl.Token) scope.Chatters {
	return scope.Chatters{
		Roster:  rosterView{roster: p.roster, broadcasterID: c.BroadcasterID},
		Exclude: p.undrawableChatters(c),
		Draws:   chatterDrawsOf(toks),

		Viewers:       p.chatterViewers(c),
		ViewerExclude: p.undrawableViewers(c),
		ViewerDraws:   viewerDrawsOf(toks),
	}
}

func (p *Pipeline) chatterViewers(c *module.Context) scope.Viewers {
	if p.viewers == nil {
		return nil
	}
	return viewerSource{rpc: p.viewers, roster: p.roster, broadcasterID: c.BroadcasterID}
}

func (p *Pipeline) undrawableChatters(c *module.Context) []uint64 {
	botID, _ := strconv.ParseUint(p.botID, 10, 64)
	return []uint64{botID, c.BroadcasterID}
}

func (p *Pipeline) undrawableViewers(c *module.Context) []uint64 {
	exclude := p.undrawableChatters(c)
	senderID, err := strconv.ParseUint(c.Env.ChatterUserID, 10, 64)
	if err == nil && senderID != 0 {
		exclude = append(exclude, senderID)
	}
	return exclude
}

func chatterDrawsOf(toks []tmpl.Token) int { return bareDrawsOf(toks, scope.RandomChatterToken) }
func viewerDrawsOf(toks []tmpl.Token) int  { return bareDrawsOf(toks, scope.RandomViewerToken) }

func bareDrawsOf(toks []tmpl.Token, name string) int {
	draws := 0
	for _, tok := range toks {
		if isBareVar(tok, name) {
			draws++
		}
	}
	return draws
}

func isBareVar(tok tmpl.Token, name string) bool {
	return tok.Kind == tmpl.KindVar && tok.Name == name && !tok.HasPayload
}

type rosterView struct {
	roster        *chatterRoster
	broadcasterID uint64
}

func (v rosterView) Chatters() []scope.Chatter {
	seen := v.roster.Snapshot(v.broadcasterID)
	out := make([]scope.Chatter, 0, len(seen))
	for i := range seen {
		out = append(out, scope.Chatter{ID: seen[i].ID, Name: chatterName(seen[i])})
	}
	return out
}

type viewerSource struct {
	rpc           ViewerLookup
	roster        *chatterRoster
	broadcasterID uint64
}

func (v viewerSource) Viewers(ctx context.Context) ([]scope.Chatter, bool) {
	entries, state := v.rpc.Snapshot(ctx, v.broadcasterID)
	if state == viewerSnapshotOK {
		return viewerChatters(entries), true
	}
	return rosterView{roster: v.roster, broadcasterID: v.broadcasterID}.Chatters(), true
}

func viewerChatters(entries []chattersSnapshotEntry) []scope.Chatter {
	out := make([]scope.Chatter, 0, len(entries))
	for _, e := range entries {
		out = append(out, scope.Chatter{ID: e.ID, Name: chatterName(Viewer{ID: e.ID, Login: e.Login, Name: e.Name})})
	}
	return out
}

func chatterName(who Viewer) string {
	name := who.Name
	if name == "" {
		name = who.Login
	}
	return sanitizeVar(name)
}
