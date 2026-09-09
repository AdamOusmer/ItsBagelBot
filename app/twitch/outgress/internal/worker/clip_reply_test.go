// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import "testing"

const testClipURL = "https://clips.twitch.tv/AbCdEf"

func TestClipReplyTextDefault(t *testing.T) {
	cases := []struct {
		name string
		meta clipMeta
		want string
	}{
		{
			name: "clipper and title",
			meta: clipMeta{Clipper: "viewer", Title: "sick play"},
			want: "viewer clipped: sick play → " + testClipURL,
		},
		{
			name: "clipper only",
			meta: clipMeta{Clipper: "viewer"},
			want: "viewer made a clip → " + testClipURL,
		},
		{
			name: "title only",
			meta: clipMeta{Title: "sick play"},
			want: "Clip: sick play → " + testClipURL,
		},
		{
			name: "neither",
			meta: clipMeta{},
			want: "New clip → " + testClipURL,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clipReplyText(tc.meta, testClipURL); got != tc.want {
				t.Errorf("clipReplyText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClipReplyTextCustomTemplate(t *testing.T) {
	meta := clipMeta{
		Clipper: "viewer",
		Title:   "sick play",
		Reply:   "{user} made {clip} titled {target}",
	}
	want := "viewer made " + testClipURL + " titled sick play"
	if got := clipReplyText(meta, testClipURL); got != want {
		t.Errorf("clipReplyText custom = %q, want %q", got, want)
	}
}

func TestClipExpand(t *testing.T) {
	// {clipper} and {title} aliases, plus an unknown token left untouched.
	got := clipExpand(clipMeta{
		Clipper: "viewer",
		Title:   "sick play",
		Reply:   "{clipper}: {title} {clip} {mystery}",
	}, testClipURL)
	want := "viewer: sick play " + testClipURL + " {mystery}"
	if got != want {
		t.Errorf("clipExpand = %q, want %q", got, want)
	}
}

func TestClipExpandCaseInsensitive(t *testing.T) {
	got := clipExpand(clipMeta{
		Clipper: "viewer",
		Title:   "sick play",
		Reply:   "{Clipper} clipped {TITLE} → {Clip} {broken",
	}, testClipURL)
	want := "viewer clipped sick play → " + testClipURL + " {broken"
	if got != want {
		t.Errorf("clipExpand case-insensitive = %q, want %q", got, want)
	}
}

// The viewer-typed title cannot mint a slash-verb through a template that
// leads with {title}/{target}: leading slashes/spaces are stripped, while
// non-leading slashes (URLs) survive.
func TestClipExpandSanitizesLeadingSlashTitle(t *testing.T) {
	got := clipExpand(clipMeta{
		Clipper: "viewer",
		Title:   " /announce pwned",
		Reply:   "{title} clipped by {clipper}",
	}, testClipURL)
	want := "announce pwned clipped by viewer"
	if got != want {
		t.Errorf("clipExpand sanitized = %q, want %q", got, want)
	}
}

// TestExpandTokensResolvesDynamic pins the behaviour CHANGE that came with
// moving the dynamic spans into pkg/tmpl: a clip or stream reply now resolves
// {random} and {choice:…} the way a sesame reward template always did.
//
// They could not before, and the reason was structural rather than deliberate:
// the dynamic vars lived in app/twitch/sesame/module, which outgress must not
// import (internal/buildguard keeps the two services apart), so this side had
// a token map and nothing else. A broadcaster reading one token list got two
// behaviours depending on which surface the line landed on.
//
// The {choice} / {choice:} pair is pinned here too: no payload names no
// options and stays literal, an empty payload resolves to "".
func TestExpandTokensResolvesDynamic(t *testing.T) {
	tokens := map[string]string{"user": "sam"}
	for _, tc := range [][2]string{
		{"{choice:only}, @{user}", "only, @sam"},
		{"[{choice:}]", "[]"},
		{"{choice}", "{choice}"},
		{"{random:7-7}", "7"},
		{"{CHOICE:Hi}", "Hi"},
		{"{unknown}", "{unknown}"},
	} {
		if got := expandTokens(tc[0], tokens); got != tc[1] {
			t.Errorf("expandTokens(%q) = %q, want %q", tc[0], got, tc[1])
		}
	}
}
