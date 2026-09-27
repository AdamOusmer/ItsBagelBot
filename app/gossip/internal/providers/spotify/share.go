// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"golang.org/x/net/html"
)

func (p *api) shareLinkFetch(tok accessToken, link string, limit int) searchFetch {
	return func(ctx context.Context) (gossiprpc.SpotifySearchReply, error) {
		var embed struct {
			HTML string `json:"html"`
		}
		// The fixed Spotify host resolves the share. Neither the share URL nor
		// returned HTML gets fetched locally, and no bearer token rides oEmbed.
		if err := p.embeds.Do(ctx, core.Request{
			Method: http.MethodGet, Path: "/oembed", Query: url.Values{"url": {link}}, NoRedirects: true,
		}, &embed); err != nil {
			return gossiprpc.SpotifySearchReply{}, err
		}
		target := embedTarget(embed.HTML)
		switch target.kind {
		case resolveTrackID:
			return p.trackByIDFetch(tok, target.id)(ctx)
		case resolveAlbumID:
			return p.albumTracks(ctx, tok, target.id, limit)
		case resolveUnsupportedLink:
			return gossiprpc.SpotifySearchReply{Error: "that Spotify link type isn't supported; share a track or album"}, nil
		default:
			return gossiprpc.SpotifySearchReply{Error: "not found on Spotify"}, nil
		}
	}
}

func embedTarget(markup string) resolvedInput {
	tokens := html.NewTokenizer(strings.NewReader(markup))
	for {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			return resolvedInput{kind: resolveText}
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		if src, ok := iframeSource(tokens.Token()); ok {
			return classifyURL(src)
		}
	}
}

func iframeSource(token html.Token) (string, bool) {
	if token.Data != "iframe" {
		return "", false
	}
	for _, attr := range token.Attr {
		if attr.Key == "src" {
			return attr.Val, true
		}
	}
	return "", false
}
