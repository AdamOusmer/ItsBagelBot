// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"net/url"
	"strings"
)

type resolveKind uint8

const (
	resolveText resolveKind = iota
	resolveTrackID
	resolveAlbumID
	resolveUnsupportedLink
)

type resolvedInput struct {
	kind resolveKind
	id   string
	text string
}

func (r resolvedInput) cacheKey() string {
	switch r.kind {
	case resolveTrackID:
		return "track:" + r.id
	case resolveAlbumID:
		return "album:" + r.id
	default:
		return "text:" + r.text
	}
}

var linkHosts = map[string]bool{
	"open.spotify.com": true,
	"play.spotify.com": true,
}

var linkKinds = map[string]resolveKind{
	"track":     resolveTrackID,
	"artist":    resolveUnsupportedLink,
	"album":     resolveAlbumID,
	"playlist":  resolveUnsupportedLink,
	"episode":   resolveUnsupportedLink,
	"show":      resolveUnsupportedLink,
	"user":      resolveUnsupportedLink,
	"audiobook": resolveUnsupportedLink,
}

func classify(raw string) resolvedInput {
	s := strings.TrimSpace(raw)
	if s == "" {
		return resolvedInput{kind: resolveText}
	}
	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(lower, "spotify:"):
		return classifyURI(s)
	case strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "http://"):
		return classifyURL(s)
	case strings.Contains(lower, "open.spotify.com/"), strings.Contains(lower, "play.spotify.com/"):
		return classifyURL("https://" + s)
	default:
		return resolvedInput{kind: resolveText, text: normalizeText(s)}
	}
}

func classifyURI(s string) resolvedInput {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return resolvedInput{kind: resolveText}
	}
	return resolveCatalogLink(linkRef{typeSeg: parts[1], id: parts[2]})
}

func classifyURL(raw string) resolvedInput {
	u, err := url.Parse(raw)
	if err != nil || !linkHosts[strings.ToLower(u.Hostname())] {
		return resolvedInput{kind: resolveText}
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	for len(segs) > 0 && strings.HasPrefix(strings.ToLower(segs[0]), "intl-") {
		segs = segs[1:]
	}
	if len(segs) != 2 {
		return resolvedInput{kind: resolveText}
	}
	return resolveCatalogLink(linkRef{typeSeg: segs[0], id: segs[1]})
}

type linkRef struct {
	typeSeg string
	id      string
}

func resolveCatalogLink(ref linkRef) resolvedInput {
	kind, ok := linkKinds[strings.ToLower(ref.typeSeg)]
	switch {
	case !ok:
		return resolvedInput{kind: resolveText}
	case kind == resolveUnsupportedLink:
		return resolvedInput{kind: kind}
	case len(ref.id) != 22 || !validCatalogID(ref.id):
		return resolvedInput{kind: resolveText}
	default:
		return resolvedInput{kind: kind, id: ref.id}
	}
}

const (
	viaTrackLink = "track_link"
	viaAlbum     = "album"
	viaFiltered  = "filtered"
	viaText      = "text"
)

type searchCandidate struct {
	q    string
	name string
}

func planTextSearch(raw string) []searchCandidate {
	s := normalizeText(raw)
	if s == "" {
		return nil
	}
	if first, ok := heuristicCandidate(s); ok {
		return []searchCandidate{first, {q: s, name: viaText}}
	}
	return []searchCandidate{{q: s, name: viaText}}
}

func heuristicCandidate(s string) (searchCandidate, bool) {
	split := splitBy(s)
	if !split.ok {
		split = splitDash(s)
		split.swap()
	}
	if !split.ok {
		return searchCandidate{}, false
	}
	return split.candidate()
}

type textSplit struct {
	left, right string
	ok          bool
}

func (t *textSplit) swap() { t.left, t.right = t.right, t.left }

func (t textSplit) candidate() (searchCandidate, bool) {
	song := strings.TrimSpace(strings.ReplaceAll(t.left, `"`, ""))
	artist := strings.TrimSpace(strings.ReplaceAll(t.right, `"`, ""))
	if song == "" || artist == "" {
		return searchCandidate{}, false
	}
	return searchCandidate{
		q:    `track:"` + song + `" artist:"` + artist + `"`,
		name: viaFiltered,
	}, true
}

func splitBy(s string) textSplit {
	fields := strings.Fields(s)
	for i := 1; i <= len(fields)-2; i++ {
		if !strings.EqualFold(fields[i], "by") {
			continue
		}
		return textSplit{left: strings.Join(fields[:i], " "), right: strings.Join(fields[i+1:], " "), ok: true}
	}
	return textSplit{}
}

func splitDash(s string) textSplit {
	i := strings.Index(s, " - ")
	if i <= 0 || i+3 >= len(s) {
		return textSplit{}
	}
	return textSplit{left: strings.TrimSpace(s[:i]), right: strings.TrimSpace(s[i+3:]), ok: true}
}

func normalizeText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
