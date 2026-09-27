// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"net/url"
	"regexp"
	"strings"
)

type resolveKind uint8

const (
	resolveText resolveKind = iota
	resolveTrackID
	resolveAlbumID
	// resolveShareLink needs oEmbed to reveal the catalog ID in a short URL.
	resolveShareLink
	resolveInvalidLink
	resolveUnsupportedLink
)

type resolvedInput struct {
	kind resolveKind
	id   string
	// text carries normalized free text, or a case-sensitive share URL.
	text string
}

func (r resolvedInput) cacheKey() string {
	switch r.kind {
	case resolveTrackID:
		return "track:" + r.id
	case resolveAlbumID:
		return "album:" + r.id
	case resolveShareLink:
		return "share:" + r.text
	default:
		// A new matcher must not reuse an old cached first-result selection.
		return "text:v3:" + r.text
	}
}

// linkHosts are the hosts whose URL paths carry catalog ids.
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

// classify distinguishes text from catalog links and short shares. Malformed
// Spotify references are refused instead of becoming fuzzy text searches.
func classify(raw string) resolvedInput {
	s := strings.TrimSpace(raw)
	if s == "" {
		return resolvedInput{kind: resolveText}
	}
	lower := strings.ToLower(s)
	var target resolvedInput
	switch {
	case strings.HasPrefix(lower, "spotify:"):
		target = classifyURI(s)
	case strings.HasPrefix(lower, "//"):
		target = classifyURL("https:" + s)
	case strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "http://"):
		target = classifyURL(s)
	case strings.HasPrefix(lower, "open.spotify.com/"), strings.HasPrefix(lower, "play.spotify.com/"),
		strings.HasPrefix(lower, "spotify.link/"), strings.HasPrefix(lower, "spoti.fi/"):
		target = classifyURL("https://" + s)
	default:
		if strings.Contains(lower, "spotify.com/") || strings.Contains(lower, "spotify.link/") || strings.Contains(lower, "spoti.fi/") {
			target = resolvedInput{kind: resolveInvalidLink}
		} else {
			target = resolvedInput{kind: resolveText}
		}
	}
	if target.kind == resolveText {
		target.text = normalizeText(s)
	}
	return target
}

func classifyURI(s string) resolvedInput {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return resolvedInput{kind: resolveInvalidLink}
	}
	return resolveCatalogLink(linkRef{typeSeg: parts[1], id: parts[2]})
}

func classifyURL(raw string) resolvedInput {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return resolvedInput{kind: resolveInvalidLink}
	}
	host := strings.ToLower(u.Hostname())
	if (linkHosts[host] || host == "spotify.link" || host == "spoti.fi") &&
		(u.User != nil || u.Port() != "") {
		return resolvedInput{kind: resolveInvalidLink}
	}
	// Short shares are resolved by Spotify's fixed oEmbed endpoint, never by
	// fuzzy-searching the URL or fetching an arbitrary user-provided host.
	if host == "spotify.link" || host == "spoti.fi" {
		return resolvedInput{kind: resolveShareLink, text: u.String()}
	}
	if !linkHosts[host] {
		return resolvedInput{kind: resolveText}
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) == 2 && segs[0] == "s" {
		return resolvedInput{kind: resolveShareLink, text: u.String()}
	}
	for len(segs) > 0 && strings.HasPrefix(strings.ToLower(segs[0]), "intl-") {
		segs = segs[1:]
	}
	if len(segs) > 0 && strings.EqualFold(segs[0], "embed") {
		segs = segs[1:]
		if len(segs) == 0 {
			return classifyURI(u.Query().Get("uri"))
		}
	}
	if len(segs) != 2 {
		return resolvedInput{kind: resolveInvalidLink}
	}
	return resolveCatalogLink(linkRef{typeSeg: segs[0], id: segs[1]})
}

type linkRef struct {
	typeSeg string
	id      string
}

// resolveCatalogLink maps one recognized reference onto a resolvedInput.
// Unsupported types are reported precisely; invalid IDs never become text
// searches that might queue another song. Catalog IDs are 22 base62 characters.
func resolveCatalogLink(ref linkRef) resolvedInput {
	kind, ok := linkKinds[strings.ToLower(ref.typeSeg)]
	switch {
	case !ok:
		return resolvedInput{kind: resolveInvalidLink}
	case kind == resolveUnsupportedLink:
		return resolvedInput{kind: kind}
	case len(ref.id) != 22 || !validCatalogID(ref.id):
		return resolvedInput{kind: resolveInvalidLink}
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
	q            string
	name         string
	song, artist string
	operators    bool
}

// planTextSearch turns chat text into ordered search candidates. The FIRST
// candidate whose metadata matches wins; plain text recovers from an
// incorrect convention split such as a title containing "by".
func planTextSearch(raw string) []searchCandidate {
	s := normalizeText(raw)
	if s == "" {
		return nil
	}
	if candidate, ok := operatorCandidate(s); ok {
		return []searchCandidate{candidate}
	}
	if first, ok := heuristicCandidate(s); ok {
		plan := []searchCandidate{first}
		if !splitBy(s).ok {
			split := splitDash(s)
			if reverse, ok := split.candidate(); ok && reverse.q != first.q {
				plan = append(plan, reverse)
			}
		}
		return append(plan, searchCandidate{q: s, name: viaText})
	}
	return []searchCandidate{{q: s, name: viaText}}
}

func heuristicCandidate(s string) (searchCandidate, bool) {
	split := splitBy(s)
	if !split.ok {
		split = splitDash(s)
		split.swap() // the dash convention orders artist first
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
		q:      `track:"` + song + `" artist:"` + artist + `"`,
		name:   viaFiltered,
		song:   song,
		artist: artist,
	}, true
}

func splitBy(s string) textSplit {
	fields := strings.Fields(s)
	for i := len(fields) - 2; i >= 1; i-- {
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

var spotifyFilter = regexp.MustCompile(`(?:^|\s)(track|artist|album|year|upc|isrc|genre|tag):(?:"([^"]+)"|(\S+))`)

func operatorCandidate(s string) (searchCandidate, bool) {
	matches := spotifyFilter.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return searchCandidate{}, false
	}
	candidate := searchCandidate{q: s, name: viaText, operators: true}
	for _, match := range matches {
		value := match[2]
		if value == "" {
			value = match[3]
		}
		switch match[1] {
		case "track":
			candidate.song = value
		case "artist":
			candidate.artist = value
		}
	}
	return candidate, true
}
