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
	target := classifyReference(inputReference{raw: s, lower: strings.ToLower(s)})
	if target.kind == resolveText {
		target.text = normalizeText(s)
	}
	return target
}

type inputReference struct {
	raw, lower string
}

func classifyReference(ref inputReference) resolvedInput {
	s, lower := ref.raw, ref.lower
	switch {
	case strings.HasPrefix(lower, "spotify:"):
		return classifyURI(s)
	case strings.HasPrefix(lower, "//"):
		return classifyURL("https:" + s)
	case ref.hasWebScheme():
		return classifyURL(s)
	case ref.hasBareSpotifyHost():
		return classifyURL("https://" + s)
	case ref.containsSpotifyHost():
		return resolvedInput{kind: resolveInvalidLink}
	default:
		return resolvedInput{kind: resolveText}
	}
}

func (ref inputReference) hasWebScheme() bool {
	return strings.HasPrefix(ref.lower, "https://") || strings.HasPrefix(ref.lower, "http://")
}

func (ref inputReference) hasBareSpotifyHost() bool {
	for _, host := range []string{"open.spotify.com/", "play.spotify.com/", "spotify.link/", "spoti.fi/"} {
		if strings.HasPrefix(ref.lower, host) {
			return true
		}
	}
	return false
}

func (ref inputReference) containsSpotifyHost() bool {
	for _, host := range []string{"spotify.com/", "spotify.link/", "spoti.fi/"} {
		if strings.Contains(ref.lower, host) {
			return true
		}
	}
	return false
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
	if err != nil {
		return resolvedInput{kind: resolveInvalidLink}
	}
	if !validSpotifyURL(u) {
		return resolvedInput{kind: resolveInvalidLink}
	}
	host := spotifyHost(strings.ToLower(u.Hostname()))
	if host.isShare() {
		return resolvedInput{kind: resolveShareLink, text: u.String()}
	}
	if !linkHosts[string(host)] {
		return resolvedInput{kind: resolveText}
	}
	return classifySpotifyPath(u)
}

type spotifyHost string

func (host spotifyHost) isShare() bool {
	return host == "spotify.link" || host == "spoti.fi"
}

func validSpotifyURL(u *url.URL) bool {
	if u.Scheme != "https" && u.Scheme != "http" {
		return false
	}
	host := spotifyHost(strings.ToLower(u.Hostname()))
	if !linkHosts[string(host)] && !host.isShare() {
		return true
	}
	return u.User == nil && u.Port() == ""
}

func classifySpotifyPath(u *url.URL) resolvedInput {
	segs := catalogPath(strings.Split(strings.Trim(u.Path, "/"), "/"))
	if len(segs) == 2 && segs[0] == "s" {
		return resolvedInput{kind: resolveShareLink, text: u.String()}
	}
	segs = stripRegionalPath(segs)
	if len(segs) > 0 && strings.EqualFold(segs[0], "embed") {
		return classifyEmbedPath(u, segs[1:])
	}
	return classifyCatalogPath(segs)
}

type catalogPath []string

func stripRegionalPath(segs catalogPath) catalogPath {
	for len(segs) > 0 && strings.HasPrefix(strings.ToLower(segs[0]), "intl-") {
		segs = segs[1:]
	}
	return segs
}

func classifyEmbedPath(u *url.URL, segs catalogPath) resolvedInput {
	if len(segs) == 0 {
		return classifyURI(u.Query().Get("uri"))
	}
	return classifyCatalogPath(segs)
}

func classifyCatalogPath(segs catalogPath) resolvedInput {
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
	s := normalizedQuery{text: normalizeText(raw)}
	if s.text == "" {
		return nil
	}
	if candidate, ok := s.operatorCandidate(); ok {
		return []searchCandidate{candidate}
	}
	if first, ok := s.heuristicCandidate(); ok {
		plan := []searchCandidate{first}
		if !s.splitBy().ok {
			split := s.splitDash()
			if reverse, ok := split.candidate(); ok && reverse.q != first.q {
				plan = append(plan, reverse)
			}
		}
		return append(plan, searchCandidate{q: s.text, name: viaText})
	}
	return []searchCandidate{{q: s.text, name: viaText}}
}

type normalizedQuery struct{ text string }

func (s normalizedQuery) heuristicCandidate() (searchCandidate, bool) {
	split := s.splitBy()
	if !split.ok {
		split = s.splitDash()
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

func (s normalizedQuery) splitBy() textSplit {
	fields := strings.Fields(s.text)
	for i := len(fields) - 2; i >= 1; i-- {
		if !strings.EqualFold(fields[i], "by") {
			continue
		}
		return textSplit{left: strings.Join(fields[:i], " "), right: strings.Join(fields[i+1:], " "), ok: true}
	}
	return textSplit{}
}

func (q normalizedQuery) splitDash() textSplit {
	s := q.text
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

func (q normalizedQuery) operatorCandidate() (searchCandidate, bool) {
	s := q.text
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
