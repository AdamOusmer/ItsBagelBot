// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fortnite

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var legacyStatKeyRe = regexp.MustCompile(`^br_(placetop1|kills|matchesplayed)_(?:keyboardmouse|gamepad|touch)_m0_playlist_(.+)$`)

var legacyCoreModes = map[string]int{
	"defaultsolo": 0, "nobuildbr_solo": 0,
	"defaultduo": 1, "nobuildbr_duo": 1,
	"defaultsquad": 2, "nobuildbr_squad": 2,
}

// aggregate folds the raw counter blob into overall and per-mode buckets using regex (legacy reference).
func aggregate(stats map[string]float64) (overall modeAgg, modes [3]modeAgg) {
	for key, val := range stats {
		m := legacyStatKeyRe.FindStringSubmatch(key)
		if m == nil {
			continue
		}
		metric, playlist := m[1], m[2]
		overall.add(metric, int64(val))
		if idx, ok := legacyCoreModes[playlist]; ok {
			modes[idx].add(metric, int64(val))
		}
	}
	return overall, modes
}

// parseRawStatsCase is one UnmarshalJSON input/output pin shared by the
// TestParseRawStats* functions below; runParseRawStatsCases is their common
// table-driven runner.
type parseRawStatsCase struct {
	name        string
	input       string
	wantErr     bool
	wantOverall modeAgg
	wantSolo    modeAgg
}

func runParseRawStatsCases(t *testing.T, tests []parseRawStatsCase) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var resp rawStatsResponse
			err := resp.UnmarshalJSON([]byte(tc.input))
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantOverall, resp.Overall)
			assert.Equal(t, tc.wantSolo, resp.Modes[0])
		})
	}
}

// TestParseRawStatsAggregation pins the happy-path counter aggregation: a
// single tracked metric sums correctly, and playlists outside the core
// modes (arena, battle-pass level) contribute to the overall bucket only,
// never to a per-mode one.
func TestParseRawStatsAggregation(t *testing.T) {
	runParseRawStatsCases(t, []parseRawStatsCase{
		{
			name: "single solo win",
			input: `{"stats": {
				"br_placetop1_keyboardmouse_m0_playlist_defaultsolo": 5
			}}`,
			wantOverall: modeAgg{wins: 5},
			wantSolo:    modeAgg{wins: 5},
		},
		{
			name: "unrelated arena and non-core playlists",
			input: `{"stats": {
				"br_placetop1_keyboardmouse_m0_playlist_habanero_solo": 3,
				"battlepass_level": 100
			}}`,
			wantOverall: modeAgg{wins: 3},
			wantSolo:    modeAgg{wins: 0},
		},
	})
}

// TestParseRawStatsNumericFormatTolerance pins tolerance for value formatting
// that a strict, allocation-free scanner is prone to trip on: whitespace
// (including a newline) between the colon and the number, scientific
// notation, and a float with a trailing ".0".
func TestParseRawStatsNumericFormatTolerance(t *testing.T) {
	runParseRawStatsCases(t, []parseRawStatsCase{
		{
			name:        "multiline formatting around colon",
			input:       "{\n  \"stats\": {\n    \"br_placetop1_keyboardmouse_m0_playlist_defaultsolo\":\n    42\n  }\n}",
			wantOverall: modeAgg{wins: 42},
			wantSolo:    modeAgg{wins: 42},
		},
		{
			name: "scientific notation and floating points",
			input: `{"stats": {
				"br_kills_gamepad_m0_playlist_nobuildbr_solo": 1.2e2,
				"br_matchesplayed_touch_m0_playlist_defaultsolo": 15.0
			}}`,
			wantOverall: modeAgg{kills: 120, matches: 15},
			wantSolo:    modeAgg{kills: 120, matches: 15},
		},
	})
}

// TestParseRawStatsMissingStatsContract pins the presence-of-"stats" contract:
// no key, a null value, or a body that never got there at all (empty, or an
// upstream error page) must all surface as errNoStatsObject rather than
// zeroed stats -- while a present-but-empty object is the legitimate shape of
// a brand new account and must not be treated as an error.
func TestParseRawStatsMissingStatsContract(t *testing.T) {
	runParseRawStatsCases(t, []parseRawStatsCase{
		{
			// No "stats" key at all is indistinguishable from a missing/malformed
			// payload -- errNoStatsObject must fire rather than serving zeros.
			name:        "empty JSON has no stats object",
			input:       "{}",
			wantErr:     true,
			wantOverall: modeAgg{},
			wantSolo:    modeAgg{},
		},
		{
			name:        "no stats object",
			input:       `{"accountId": "123"}`,
			wantErr:     true,
			wantOverall: modeAgg{},
			wantSolo:    modeAgg{},
		},
		{
			// A present-but-empty "stats" object is a legitimate response shape
			// (e.g. a brand new account) and must not be treated as an error.
			name:        "stats empty object",
			input:       `{"accountId": "123", "stats": {}}`,
			wantErr:     false,
			wantOverall: modeAgg{},
			wantSolo:    modeAgg{},
		},
		{
			// Regression for bug 1: an empty response body must not silently
			// resolve to zeroed stats.
			name:    "empty body",
			input:   "",
			wantErr: true,
		},
		{
			// Regression for bug 1: an upstream error page (e.g. a gateway 502)
			// has no "stats" object and must surface as an error.
			name:    "html error page body",
			input:   "<html>502 Bad Gateway</html>",
			wantErr: true,
		},
		{
			// Regression for bug 1: "stats" present but null has no locatable
			// '{', which must still be treated as missing rather than empty.
			name:    "stats value is null",
			input:   `{"stats":null}`,
			wantErr: true,
		},
	})
}

// TestParseRawStatsScannerSkipsNonMetricSiblings pins the scanner's ability to
// consume a non-trivial sibling value as a whole unit -- a nested object, an
// array (itself holding a nested object and a string with a stray brace), or
// a string with an embedded comma/brace/escaped quote -- without
// desynchronizing and losing (or misreading) the metric key that follows.
// The truncated-json case is the same property from the opposite direction:
// running off the end of the buffer after a complete pair must still yield
// the value already read, not corrupt it.
func TestParseRawStatsScannerSkipsNonMetricSiblings(t *testing.T) {
	runParseRawStatsCases(t, []parseRawStatsCase{
		{
			// Truncation after a complete key/value pair still yields the value
			// already read; the object never closes so the scanner simply runs
			// off the end of the buffer. This is a deliberate, low-risk
			// tradeoff -- a truncated body is a rare transport failure, and the
			// severe cases (missing/empty body, non-JSON, no stats field) are
			// already covered by TestParseRawStatsMissingStatsContract.
			name:        "malformed truncated json",
			input:       `{"stats": {"br_placetop1_keyboardmouse_m0_playlist_defaultsolo": 10`,
			wantOverall: modeAgg{wins: 10},
			wantSolo:    modeAgg{wins: 10},
		},
		{
			// Regression for bug 2: a nested object sibling must be skipped as a
			// whole balanced unit, not treated as terminating the scan at its
			// first '}'. The key after the nested object must still be counted.
			name: "nested object sibling does not truncate the scan",
			input: `{"stats":{
				"br_placetop1_gamepad_m0_playlist_defaultsolo": 5,
				"nested": {"x": 1},
				"br_kills_gamepad_m0_playlist_defaultsolo": 99
			}}`,
			wantOverall: modeAgg{wins: 5, kills: 99},
			wantSolo:    modeAgg{wins: 5, kills: 99},
		},
		{
			// Regression for bug 2: an array sibling (with a nested object and a
			// string containing a stray '}' inside it) must also be skipped as a
			// whole unit rather than desynchronizing the scanner.
			name: "array sibling does not truncate the scan",
			input: `{"stats":{
				"tags": [1, 2, {"x": 3}, "str}anger"],
				"br_matchesplayed_touch_m0_playlist_defaultsolo": 4
			}}`,
			wantOverall: modeAgg{matches: 4},
			wantSolo:    modeAgg{matches: 4},
		},
		{
			// Regression for bug 2: a string-valued sibling containing a comma,
			// a brace lookalike, and an escaped quote must be consumed whole
			// rather than desynchronizing the scanner via readNumber's old
			// stop-at-comma/brace behavior.
			name:        "string valued sibling does not desync the scan",
			input:       `{"stats":{"weirdString":"a,b}c\"d","br_kills_gamepad_m0_playlist_defaultsolo":7}}`,
			wantOverall: modeAgg{kills: 7},
			wantSolo:    modeAgg{kills: 7},
		},
	})
}

// TestParseNumberBytesOverflowGuard is a regression test for bug 3: a token
// too large for int64 accumulation must fall back to strconv.ParseFloat
// rather than silently wrapping via signed integer overflow.
//
// The fallback's own float64->int64 conversion is implementation-defined for
// out-of-range values (Go spec section on conversions), and differs by
// architecture (e.g. amd64 vs arm64 saturate/wrap differently), so this test
// deliberately does not assert an exact resulting value. Instead it proves
// the guard changed behavior at all: it reproduces the old unguarded
// accumulation (whose signed-overflow wraparound *is* spec-guaranteed and
// portable) and asserts the guarded result no longer matches it.
func TestParseNumberBytesOverflowGuard(t *testing.T) {
	const overflowToken = "99999999999999999999" // far beyond math.MaxInt64

	var naive int64
	for _, c := range overflowToken {
		naive = naive*10 + int64(c-'0')
	}

	got := parseNumberBytes([]byte(overflowToken))

	assert.NotEqual(t, naive, got, "overflow guard should not silently wrap like the old accumulator")
}

// TestParseRawStatsIntegerOverflowToken exercises bug 3's guard through the
// full scan path (not just parseNumberBytes in isolation), confirming an
// oversized token in a real stat entry doesn't poison the scan or panic.
func TestParseRawStatsIntegerOverflowToken(t *testing.T) {
	input := `{"stats":{"br_kills_gamepad_m0_playlist_defaultsolo":99999999999999999999}}`

	var resp rawStatsResponse
	require.NoError(t, resp.UnmarshalJSON([]byte(input)))

	var naive int64
	for _, c := range "99999999999999999999" {
		naive = naive*10 + int64(c-'0')
	}
	assert.NotEqual(t, naive, resp.Overall.kills, "overflow guard should not silently wrap like the old accumulator")
}
