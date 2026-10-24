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

func parseBotUserID(raw string) uint64 {
	botID, _ := strconv.ParseUint(raw, 10, 64)
	return botID
}

func (p *Pipeline) undrawableChatters(c *module.Context) []uint64 {
	return []uint64{p.botUserID, c.BroadcasterID}
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

func (v rosterView) View(read func(scope.RosterView)) {
	v.roster.View(v.broadcasterID, read)
}

func (v rosterView) chatters() []scope.Chatter {
	var out []scope.Chatter
	v.View(func(view scope.RosterView) {
		out = make([]scope.Chatter, view.Len())
		for i := range out {
			out[i] = view.At(i)
		}
	})
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
	return rosterView{roster: v.roster, broadcasterID: v.broadcasterID}.chatters(), true
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
