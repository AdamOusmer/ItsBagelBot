// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fortnite

import (
	"bytes"
	"errors"

	"ItsBagelBot/pkg/codec"
)

var errNoStatsObject = errors.New("fortnite: stats object missing from response")

type rawStatsResponse struct {
	Overall modeAgg
	Modes   [3]modeAgg
}

func (r *rawStatsResponse) UnmarshalJSON(data []byte) error {
	overall, modes, err := parseRawStats(data)
	if err != nil {
		return err
	}
	r.Overall, r.Modes = overall, modes
	return nil
}

var (
	prefixBR            = []byte("br_")
	prefixPlacetop1     = []byte("placetop1_")
	prefixKills         = []byte("kills_")
	prefixMatchesPlayed = []byte("matchesplayed_")

	prefixKBM     = []byte("keyboardmouse_m0_playlist_")
	prefixGamepad = []byte("gamepad_m0_playlist_")
	prefixTouch   = []byte("touch_m0_playlist_")

	plDefaultSolo  = []byte("defaultsolo")
	plNoBuildSolo  = []byte("nobuildbr_solo")
	plDefaultDuo   = []byte("defaultduo")
	plNoBuildDuo   = []byte("nobuildbr_duo")
	plDefaultSquad = []byte("defaultsquad")
	plNoBuildSquad = []byte("nobuildbr_squad")
)

const (
	metricWins    = 1
	metricKills   = 2
	metricMatches = 3
)

func parseNumberBytes(raw []byte) int64 {
	if val, err := codec.ParseInt(raw); err == nil {
		return val
	}
	f, err := codec.ParseFloat(raw)
	if err != nil {
		return 0
	}
	return int64(f)
}

func matchMetricPrefix(s []byte) (int, []byte, bool) {
	switch {
	case bytes.HasPrefix(s, prefixPlacetop1):
		return metricWins, s[len(prefixPlacetop1):], true
	case bytes.HasPrefix(s, prefixKills):
		return metricKills, s[len(prefixKills):], true
	case bytes.HasPrefix(s, prefixMatchesPlayed):
		return metricMatches, s[len(prefixMatchesPlayed):], true
	default:
		return 0, nil, false
	}
}

func stripInputPrefix(s []byte) ([]byte, bool) {
	switch {
	case bytes.HasPrefix(s, prefixKBM):
		return s[len(prefixKBM):], true
	case bytes.HasPrefix(s, prefixGamepad):
		return s[len(prefixGamepad):], true
	case bytes.HasPrefix(s, prefixTouch):
		return s[len(prefixTouch):], true
	default:
		return nil, false
	}
}

func matchStatKeyBytes(key []byte) (int, []byte, bool) {
	if !bytes.HasPrefix(key, prefixBR) {
		return 0, nil, false
	}
	metric, s, ok := matchMetricPrefix(key[len(prefixBR):])
	if !ok {
		return 0, nil, false
	}
	pl, ok := stripInputPrefix(s)
	if !ok || len(pl) == 0 {
		return 0, nil, false
	}
	return metric, pl, true
}

func coreModeIndexBytes(pl []byte) (int, bool) {
	switch {
	case bytes.Equal(pl, plDefaultSolo) || bytes.Equal(pl, plNoBuildSolo):
		return 0, true
	case bytes.Equal(pl, plDefaultDuo) || bytes.Equal(pl, plNoBuildDuo):
		return 1, true
	case bytes.Equal(pl, plDefaultSquad) || bytes.Equal(pl, plNoBuildSquad):
		return 2, true
	default:
		return -1, false
	}
}

func addMetric(a *modeAgg, metric int, val int64) {
	switch metric {
	case metricWins:
		a.wins += val
	case metricKills:
		a.kills += val
	case metricMatches:
		a.matches += val
	}
}

var statsPath = codec.Path{"stats"}

type statsFold struct {
	overall modeAgg
	modes   [3]modeAgg
	entered bool
}

func (f *statsFold) fold(key, value []byte, kind codec.Kind) error {
	f.entered = true
	if kind != codec.KindNumber {
		return nil
	}
	metric, pl, ok := matchStatKeyBytes(key)
	if !ok {
		return nil
	}
	val := parseNumberBytes(value)
	addMetric(&f.overall, metric, val)
	if idx, ok := coreModeIndexBytes(pl); ok {
		addMetric(&f.modes[idx], metric, val)
	}
	return nil
}

func parseRawStats(data []byte) (modeAgg, [3]modeAgg, error) {
	var f statsFold
	if err := codec.ExtractEach(data, f.fold, statsPath); err != nil && !f.entered {
		return modeAgg{}, [3]modeAgg{}, errNoStatsObject
	}
	return f.overall, f.modes, nil
}
