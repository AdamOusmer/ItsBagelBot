// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"net/url"
	"strings"
)

// resolveKind classifies what one search input turned out to be. The kinds
// decide both the cheapest upstream route (a pasted link resolves by id in
// one immutable-object fetch; text searches) and how long the answer may be
// cached (ids are forever; text answers drift with the catalog).
type resolveKind uint8

const (
	// resolveText is anything that is not recognizably a Spotify reference.
	resolveText resolveKind = iota
	resolveTrackID
	resolveArtistID
	resolveAlbumID
	// resolveUnsupportedLink marks a well-formed Spotify link to a type this
	// provider deliberately does not serve (playlists, podcasts, audiobooks).
	// It is reported as such rather than searched as text: sharing a podcast
	// episode should not return songs that merely share its words.
	resolveUnsupportedLink
)

// resolvedInput is the outcome of classifying one search input.
type resolvedInput struct {
	kind resolveKind
	// id carries the bare catalog id (validated base62) for the *_ID kinds.
	id string
	// text carries the normalized free text for resolveText.
	text string
}

// cacheKey is the canonical identity of the input for cache purposes: every
// spelling that resolves to the same thing shares one entry (the same track
// linked by URL, URI or regional deep link collapses onto one key), while
// text keys on the whitespace-normalized phrase.
func (r resolvedInput) cacheKey() string {
	switch r.kind {
	case resolveTrackID:
		return "track:" + r.id
	case resolveArtistID:
		return "artist:" + r.id
	case resolveAlbumID:
		return "album:" + r.id
	default:
		return "text:" + r.text
	}
}

// linkHosts are the hosts whose URL paths carry catalog ids. spotify.link /
// spoti.fi shorteners are deliberately absent: they interstitial through JS,
// so following them lands on HTML no JSON decoder can read.
var linkHosts = map[string]bool{
	"open.spotify.com": true,
	"play.spotify.com": true,
}

// linkKinds maps the link/URI type segment onto a resolve kind. Types present
// but mapped to resolveUnsupportedLink are recognized so they can be reported
// precisely instead of silently degraded to a text search.
var linkKinds = map[string]resolveKind{
	"track":     resolveTrackID,
	"artist":    resolveArtistID,
	"album":     resolveAlbumID,
	"playlist":  resolveUnsupportedLink,
	"episode":   resolveUnsupportedLink,
	"show":      resolveUnsupportedLink,
	"user":      resolveUnsupportedLink,
	"audiobook": resolveUnsupportedLink,
}

// classify inspects one raw search input and decides how the provider will
// resolve it. Classification never errors: anything it cannot pin down
// degrades to resolveText, which is always servable.
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
		// Schemeless paste ("open.spotify.com/track/x"): parse tolerantly.
		return classifyURL("https://" + s)
	default:
		return resolvedInput{kind: resolveText, text: normalizeText(s)}
	}
}

// classifyURI parses the spotify:<type>:<id> URI form. The id segment must be
// taken verbatim — base62 ids are case-sensitive, so nothing here may fold
// case after the type token.
func classifyURI(s string) resolvedInput {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return resolvedInput{kind: resolveText}
	}
	return resolveCatalogLink(linkRef{typeSeg: parts[1], id: parts[2]})
}

// classifyURL parses an open/play.spotify.com page URL, including the
// regional deep-link form (.../intl-<region>/track/<id>) and any tracking
// query (which url.Parse discards).
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

// catalogTarget validates one parsed link target against its resolve kind.
// Recognized-but-unsupported types are reported precisely; ids must be REAL
// catalog ids — Spotify's are always 22 base62 characters, so anything else
// is a typo-shaped phrase better served by a text search than by a
// guaranteed 404. An unrecognized segment reads as the zero kind and degrades
// to text like any other unclassifiable input.
// linkRef is a recognized Spotify reference pulled out of an input: the type
// segment exactly as written (its case decides nothing here) plus the id
// verbatim — base62 ids are case-sensitive, so nothing may fold them.
type linkRef struct {
	typeSeg string
	id      string
}

// resolveCatalogLink maps one recognized reference onto a resolvedInput.
// Recognized-but-unsupported types are reported precisely; ids must be REAL
// catalog ids — Spotify's are always 22 base62 characters, so anything else
// is a typo-shaped phrase better served by a text search than by a
// guaranteed 404. An unrecognized segment reads as the zero kind and degrades
// to text like any other unclassifiable input.
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

// Tokens reported in SpotifySearchReply.ResolvedAs, telling the caller how an
// input was interpreted: which upstream shape answered and whether the match
// was exact (a link id) or best-effort (text).
const (
	viaTrackLink = "track_link"
	viaArtistTop = "artist_top"
	viaAlbum     = "album"
	viaFiltered  = "filtered"
	viaText      = "text"
)

// searchCandidate is one upstream query attempt in a text plan.
type searchCandidate struct {
	// q is the literal search string sent upstream.
	q string
	// name is the ResolvedAs token this candidate reports on success.
	name string
}

// planTextSearch turns chat text into ordered search candidates. The FIRST
// candidate that returns tracks wins; later ones are degradations. Today's
// plans are at most two deep (field-filtered, then plain), so a mis-split
// costs one cheap empty search, never a wrong answer served confidently.
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

// heuristicCandidate builds the field-scoped first candidate from whichever
// naming convention the input follows — "song by artist" first, then the
// dash form copied from video titles. ok is false when neither convention
// matches or a split leaves a usable side empty; the caller then plans the
// plain search alone. A false positive ("Stand by Me") survives as the
// fallback candidate, which is why the two-step plan exists.
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

// textSplit is the two halves of one convention split (song left / artist
// right for the by-form; the dash form swaps before building its candidate);
// ok reports whether the separator was found with content on both sides.
type textSplit struct {
	left, right string
	ok          bool
}

// swap flips which half leads, for conventions that order the artist first.
func (t *textSplit) swap() { t.left, t.right = t.right, t.left }

// candidate shapes the split into Spotify's field-scoped query. Inner quotes
// are stripped (they would terminate the qualifier's phrase early); a side
// left empty after stripping means the heuristic misfired and the candidate
// is dropped entirely rather than sent malformed.
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

// splitBy splits s around its first standalone "by", requiring at least one
// field on each side. A false positive ("Stand by Me") survives as the
// fallback candidate, which is why the two-step plan exists.
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

// splitDash splits on the spaced-hyphen convention copied from video titles;
// the LEFT side is treated as the artist. Only the first separator splits, so
// "A - B - Remix" reads artist "A", song "B - Remix".
func splitDash(s string) textSplit {
	i := strings.Index(s, " - ")
	if i <= 0 || i+3 >= len(s) {
		return textSplit{}
	}
	return textSplit{left: strings.TrimSpace(s[:i]), right: strings.TrimSpace(s[i+3:]), ok: true}
}

// normalizeText folds case and collapses runs of whitespace, so "Mr.
// Brightside", "mr   brightside" and "MR BRIGHTSIDE" share one cache entry
// and one upstream query (catalog search itself is case-insensitive).
func normalizeText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
