// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/pkg/tmpl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderPure expands a template through a chain carrying only the pure scope,
// which is what the engine mounts first on every custom command.
func renderPure(t *testing.T, p Pure, template string) string {
	t.Helper()
	chain := Chain{p}
	toks := tmpl.Lex(template)
	values := chain.Plan(context.Background(), toks, nil)
	return string(chain.Render(nil, toks, values))
}

// pinnedNow is the clock the countdown tests measure against: an assertion
// against time.Now() would either be flaky or be a restatement of the code.
var pinnedNow = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

func fixedClock() Pure {
	return Pure{Now: func() time.Time { return pinnedNow }}
}

func TestPureOwnsTheUtilityPalette(t *testing.T) {
	for _, name := range []string{
		"random", "choice", "math", "queryescape", "pathescape",
		"repeat", "countdown", "countup",
	} {
		assert.True(t, Pure{}.Owns(name), "pure should own %q", name)
	}
	for _, name := range []string{"user", "args", "counter", "querystring", "maths", ""} {
		assert.False(t, Pure{}.Owns(name), "pure should not own %q", name)
	}
}

func TestPureUtilitiesResolveAtRenderTime(t *testing.T) {
	// The pure scope is the one whose Values is not a map: two spans of the
	// same name must each be evaluated, not looked up once. {math} makes that
	// checkable without a coin flip.
	assert.Equal(t, "3 and 7", renderPure(t, Pure{}, "{math:1+2} and {math:3+4}"))
}

func TestCountdownCountsTowardTheDate(t *testing.T) {
	cases := []struct {
		name     string
		template string
		want     string
	}{
		{"days and hours remain", "{countdown:2026-09-12T16:00:00Z}", "3 days, 4 hours"},
		{"a bare date is UTC midnight", "{countdown:2026-09-11}", "1 day, 12 hours"},
		{"an offset is honoured", "{countdown:2026-09-09T15:00:00+01:00}", "2 hours"},
		{"a passed date clamps to zero", "{countdown:2020-01-01}", "less than a minute"},
		{"time since a past date", "{countup:2026-09-08T12:00:00Z}", "1 day"},
		{"a future date clamps to zero", "{countup:2030-01-01}", "less than a minute"},
		{"surrounding spaces are trimmed", "{countdown: 2026-09-11 }", "1 day, 12 hours"},
		{"a non-date renders nothing", "{countdown:next tuesday}", ""},
		{"a partial date renders nothing", "{countdown:2026-09}", ""},
		{"an impossible date renders nothing", "{countdown:2026-13-45}", ""},
		{"an invalid date renders its fallback", "{countdown:soon|soon™}", "soon™"},
		{"a bare countdown is not the token", "{countdown}", "{countdown}"},
		{"a bare countup is not the token", "{countup}", "{countup}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, renderPure(t, fixedClock(), tc.template))
		})
	}
}

func TestCountdownWordsItselfInTheBroadcasterLocale(t *testing.T) {
	// The wording comes from the shared catalog humanizer (!uptime prints the
	// same words), so a French channel must not get an English countdown.
	p := fixedClock()
	p.Locale = "fr"
	assert.Equal(t, "3 jours, 4 heures", renderPure(t, p, "{countdown:2026-09-12T16:00:00Z}"))
}

func TestCountdownDefaultsToTheWallClock(t *testing.T) {
	// A zero-value Pure (what every existing caller builds) still resolves:
	// the injectable clock is for tests, not a required dependency.
	got := renderPure(t, Pure{}, "{countup:2020-01-01}")
	assert.NotEmpty(t, got)
	assert.NotContains(t, got, "{")
}

func TestRepeatRefusesAnOverlongLine(t *testing.T) {
	// 20 x 24 bytes + 19 separators = 499, over the 480-byte line budget, so
	// the span resolves to nothing (and would render a fallback) rather than
	// handing Twitch a message it silently drops.
	phrase := "123456789012345678901234"
	require.Len(t, phrase, 24)
	assert.Equal(t, "", renderPure(t, Pure{}, "{repeat:20:"+phrase+"}"))
	// One byte shorter fits: 20 x 23 + 19 = 479.
	assert.NotEmpty(t, renderPure(t, Pure{}, "{repeat:20:"+phrase[:23]+"}"))
}

func TestQuerystringEscapesTheWholeArgumentString(t *testing.T) {
	msg := Message{Args: "hello world & friends"}
	chain := Chain{Pure{}, msg}
	toks := tmpl.Lex("{querystring} {queryescape:hello world & friends}")
	values := chain.Plan(context.Background(), toks, nil)
	out := string(chain.Render(nil, toks, values))
	// The alias and the general encoder must produce the same bytes: one
	// encoder, two spellings.
	assert.Equal(t, "hello+world+%26+friends hello+world+%26+friends", out)
}

func TestQuerystringIsEmptyWithoutArguments(t *testing.T) {
	// Empty (ok=true), not literal: a command run with no arguments has to
	// render the span's fallback, like every other message token.
	chain := Chain{Message{}}
	toks := tmpl.Lex("[{querystring}] [{querystring|none}]")
	values := chain.Plan(context.Background(), toks, nil)
	assert.Equal(t, "[] [none]", string(chain.Render(nil, toks, values)))
}

func TestQuerystringRejectsAPayload(t *testing.T) {
	// {querystring:x} is not a token: it stays literal like any other name
	// the palette does not have.
	chain := Chain{Message{Args: "hi"}}
	toks := tmpl.Lex("{querystring:x}")
	values := chain.Plan(context.Background(), toks, nil)
	assert.Equal(t, "{querystring:x}", string(chain.Render(nil, toks, values)))
}
