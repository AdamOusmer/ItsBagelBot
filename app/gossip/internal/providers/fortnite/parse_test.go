// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fortnite

import (
	"os"
	"regexp"
	"testing"

	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var legacyStatKeyRe = regexp.MustCompile(`^br_(placetop1|kills|matchesplayed)_(?:keyboardmouse|gamepad|touch)_m0_playlist_(.+)$`)

var legacyCoreModes = map[string]int{
	"defaultsolo": 0, "nobuildbr_solo": 0,
	"defaultduo": 1, "nobuildbr_duo": 1,
	"defaultsquad": 2, "nobuildbr_squad": 2,
}

func (a *modeAgg) add(metric string, v int64) {
	switch metric {
	case "placetop1":
		a.wins += v
	case "kills":
		a.kills += v
	case "matchesplayed":
		a.matches += v
	}
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

func TestParseRawStats(t *testing.T) {
	for _, tc := range []struct {
		name        string
		input       string
		wantErr     bool
		wantOverall modeAgg
		wantSolo    modeAgg
	}{
		{"counts a single solo win", `{"stats": {"br_placetop1_keyboardmouse_m0_playlist_defaultsolo": 5}}`, false,
			modeAgg{wins: 5}, modeAgg{wins: 5}},
		{"counts arena and non-core playlists overall but not per mode", `{"stats": {
			"br_placetop1_keyboardmouse_m0_playlist_habanero_solo": 3,
			"battlepass_level": 100
		}}`, false, modeAgg{wins: 3}, modeAgg{}},
		{"tolerates multiline formatting around the colon",
			"{\n  \"stats\": {\n    \"br_placetop1_keyboardmouse_m0_playlist_defaultsolo\":\n    42\n  }\n}", false,
			modeAgg{wins: 42}, modeAgg{wins: 42}},
		{"reads scientific notation and floating points", `{"stats": {
			"br_kills_gamepad_m0_playlist_nobuildbr_solo": 1.2e2,
			"br_matchesplayed_touch_m0_playlist_defaultsolo": 15.0
		}}`, false, modeAgg{kills: 120, matches: 15}, modeAgg{kills: 120, matches: 15}},
		{"rejects an empty json object", "{}", true, modeAgg{}, modeAgg{}},
		{"rejects a response without a stats object", `{"accountId": "123"}`, true, modeAgg{}, modeAgg{}},
		{"accepts an empty stats object", `{"accountId": "123", "stats": {}}`, false, modeAgg{}, modeAgg{}},
		{"rejects an empty body", "", true, modeAgg{}, modeAgg{}},
		{"rejects an html error page", "<html>502 Bad Gateway</html>", true, modeAgg{}, modeAgg{}},
		{"rejects a null stats value", `{"stats":null}`, true, modeAgg{}, modeAgg{}},
		{"keeps what a truncated response already said",
			`{"stats": {"br_placetop1_keyboardmouse_m0_playlist_defaultsolo": 10`, false, modeAgg{wins: 10}, modeAgg{wins: 10}},
		{"a nested object sibling does not truncate the scan", `{"stats":{
			"br_placetop1_gamepad_m0_playlist_defaultsolo": 5,
			"nested": {"x": 1},
			"br_kills_gamepad_m0_playlist_defaultsolo": 99
		}}`, false, modeAgg{wins: 5, kills: 99}, modeAgg{wins: 5, kills: 99}},
		{"an array sibling does not truncate the scan", `{"stats":{
			"tags": [1, 2, {"x": 3}, "str}anger"],
			"br_matchesplayed_touch_m0_playlist_defaultsolo": 4
		}}`, false, modeAgg{matches: 4}, modeAgg{matches: 4}},
		{"a string valued sibling does not desync the scan",
			`{"stats":{"weirdString":"a,b}c\"d","br_kills_gamepad_m0_playlist_defaultsolo":7}}`, false,
			modeAgg{kills: 7}, modeAgg{kills: 7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resp rawStatsResponse
			err := resp.UnmarshalJSON([]byte(tc.input))

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.wantOverall, resp.Overall)
			assert.Equal(t, tc.wantSolo, resp.Modes[0])
		})
	}
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

func TestStatsRealBlobAggregation(t *testing.T) {
	body, err := os.ReadFile("testdata/stats_v2_real.json")
	require.NoError(t, err)
	var resp rawStatsResponse
	require.NoError(t, codec.Unmarshal(body, &resp))

	wantOverall := modeAgg{wins: 11472, matches: 33287, kills: 221742}
	wantModes := [3]modeAgg{
		{wins: 3290, matches: 11645, kills: 82607},
		{wins: 3668, matches: 8699, kills: 61606},
		{wins: 2954, matches: 7350, kills: 47051},
	}

	assert.Equal(t, wantOverall, resp.Overall)
	assert.Equal(t, wantModes, resp.Modes)

	var mapResp struct {
		Stats map[string]float64 `json:"stats"`
	}
	require.NoError(t, codec.Unmarshal(body, &mapResp))
	mapOverall, mapModes := aggregate(mapResp.Stats)
	assert.Equal(t, wantOverall, mapOverall)
	assert.Equal(t, wantModes, mapModes)
}

func BenchmarkStatsZeroAllocUnmarshal(b *testing.B) {
	data, err := os.ReadFile("testdata/stats_v2_real.json")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var resp rawStatsResponse
		if err := resp.UnmarshalJSON(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStatsLegacyMapAggregate(b *testing.B) {
	data, err := os.ReadFile("testdata/stats_v2_real.json")
	if err != nil {
		b.Fatal(err)
	}
	var mapResp struct {
		Stats map[string]float64 `json:"stats"`
	}
	if err := codec.Unmarshal(data, &mapResp); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = aggregate(mapResp.Stats)
	}
}
