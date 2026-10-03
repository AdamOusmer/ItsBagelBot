// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var pinnedNow = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

func fixedClock() Pure {
	return Pure{Now: func() time.Time { return pinnedNow }}
}

func TestCountdownAndCountup(t *testing.T) {
	tests := []struct{ name, template, want string }{
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, render(t, tt.template, Chain{fixedClock()}, nil))
		})
	}
}

func TestCountdownWordsItselfInTheBroadcasterLocale(t *testing.T) {
	p := fixedClock()
	p.Locale = "fr"
	assert.Equal(t, "3 jours, 4 heures", render(t, "{countdown:2026-09-12T16:00:00Z}", Chain{p}, nil))
}

func TestCountdownDefaultsToTheWallClock(t *testing.T) {
	got := render(t, "{countup:2020-01-01}", Chain{Pure{}}, nil)
	assert.NotEmpty(t, got)
	assert.NotContains(t, got, "{")
}

func TestRepeatRefusesAnOverlongLine(t *testing.T) {
	phrase := "123456789012345678901234"
	require.Len(t, phrase, 24)
	assert.Equal(t, "", render(t, "{repeat:20:"+phrase+"}", Chain{Pure{}}, nil))
	assert.NotEmpty(t, render(t, "{repeat:20:"+phrase[:23]+"}", Chain{Pure{}}, nil))
}

func TestPureResolvesEachSpanIndependently(t *testing.T) {
	chain := Chain{Pure{}}
	assert.Equal(t, "7", render(t, "{random:7-7}", chain, nil))
	assert.Equal(t, "only", render(t, "{choice:only}", chain, nil))
	assert.Equal(t, "{choice}", render(t, "{choice}", chain, nil), "no options named")
	assert.Equal(t, "{random:x-y}", render(t, "{random:x-y}", chain, nil), "unparseable range")

	rolls := map[string]struct{}{}
	for range 40 {
		rolls[render(t, "{random:1-1000}{random:1-1000}", chain, nil)] = struct{}{}
	}
	assert.Greater(t, len(rolls), 1, "two spans of one pure token draw independently")
}
