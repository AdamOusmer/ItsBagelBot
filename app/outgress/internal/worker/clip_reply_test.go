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
	got := clipExpand("{clipper}: {title} {clip} {mystery}", "viewer", "sick play", testClipURL)
	want := "viewer: sick play " + testClipURL + " {mystery}"
	if got != want {
		t.Errorf("clipExpand = %q, want %q", got, want)
	}
}
