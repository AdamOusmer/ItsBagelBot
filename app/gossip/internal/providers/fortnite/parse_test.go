// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fortnite

import (
	"ItsBagelBot/app/gossip/internal/core"
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

func TestParseRawStatsMissingStatsContract(t *testing.T) {
	runParseRawStatsCases(t, []parseRawStatsCase{
		{
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
			name:        "stats empty object",
			input:       `{"accountId": "123", "stats": {}}`,
			wantErr:     false,
			wantOverall: modeAgg{},
			wantSolo:    modeAgg{},
		},
		{
			name:    "empty body",
			input:   "",
			wantErr: true,
		},
		{
			name:    "html error page body",
			input:   "<html>502 Bad Gateway</html>",
			wantErr: true,
		},
		{
			name:    "stats value is null",
			input:   `{"stats":null}`,
			wantErr: true,
		},
	})
}

func TestParseRawStatsScannerSkipsNonMetricSiblings(t *testing.T) {
	runParseRawStatsCases(t, []parseRawStatsCase{
		{
			name:        "malformed truncated json",
			input:       `{"stats": {"br_placetop1_keyboardmouse_m0_playlist_defaultsolo": 10`,
			wantOverall: modeAgg{wins: 10},
			wantSolo:    modeAgg{wins: 10},
		},
		{
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
			name: "array sibling does not truncate the scan",
			input: `{"stats":{
				"tags": [1, 2, {"x": 3}, "str}anger"],
				"br_matchesplayed_touch_m0_playlist_defaultsolo": 4
			}}`,
			wantOverall: modeAgg{matches: 4},
			wantSolo:    modeAgg{matches: 4},
		},
		{
			name:        "string valued sibling does not desync the scan",
			input:       `{"stats":{"weirdString":"a,b}c\"d","br_kills_gamepad_m0_playlist_defaultsolo":7}}`,
			wantOverall: modeAgg{kills: 7},
			wantSolo:    modeAgg{kills: 7},
		},
	})
}

func TestParseNumberBytesOverflowGuard(t *testing.T) {
	const overflowToken = "99999999999999999999"

	var naive int64
	for _, c := range overflowToken {
		naive = naive*10 + int64(c-'0')
	}

	got := parseNumberBytes([]byte(overflowToken))

	assert.NotEqual(t, naive, got, "overflow guard should not silently wrap like the old accumulator")
}

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

func init() { core.SetSSRFCheckForTests(false) }
