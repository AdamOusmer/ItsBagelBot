// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fortnite

import (
	"bytes"
	"errors"

	"ItsBagelBot/pkg/codec"
)

// errNoStatsObject is returned when the response body has no locatable "stats"
// object (empty body, an error page, or a payload missing the field). Callers
// must treat this as a hard failure rather than serving zeroed aggregates.
var errNoStatsObject = errors.New("fortnite: stats object missing from response")

// rawStatsResponse is the /api/v2/stats/{accountId} body: Epic's raw stats-v2
// counters. It implements json.Unmarshaler so the ~1000-key counter blob folds
// straight into overall and per-mode aggregates on decode, without ever
// materializing the map, strings, and boxed floats a struct decode would build
// for the counters nobody reads.
type rawStatsResponse struct {
	Overall modeAgg
	Modes   [3]modeAgg
}

// UnmarshalJSON folds the raw stats JSON into the aggregates in one pass.
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

// parseNumberBytes decodes one counter value. Epic writes most counters as
// plain integers but not all of them -- floats and scientific notation both
// appear in real bodies -- so anything the integer decoder rejects is retried
// as a float. A token past int64 is rejected the same way and lands on that
// float path too, which is the point: an unrepresentable counter must not wrap
// around into a plausible-looking negative. A value neither decoder accepts
// folds to 0, since one malformed counter out of a thousand is not worth
// failing the whole lookup over.
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

// matchMetricPrefix matches and extracts metric constant and remainder.
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

// stripInputPrefix removes the input device identifier prefix.
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

// matchStatKeyBytes matches a raw stats key byte slice.
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

// coreModeIndexBytes maps a playlist byte slice to core mode breakdown (0: solo, 1: duo, 2: squad).
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

// statsPath is built once rather than per call: a path built at the call site
// is the one heap allocation the whole parse would otherwise make. It is
// read-only; the extract API never writes through it.
var statsPath = codec.Path{"stats"}

// statsFold is the aggregation state the walk carries. It is one struct so the
// callback closes over a single pointer instead of four separate variables.
type statsFold struct {
	overall modeAgg
	modes   [3]modeAgg
	// entered records that the walk reached at least one member of the stats
	// object, which is what separates "no stats here" from "stats truncated
	// partway through". See parseRawStats.
	entered bool
}

// fold matches one raw counter and accumulates it. The number is decoded only
// after the key routes somewhere: the blob is a thousand counters wide and a
// couple dozen of them are tracked, so decoding every value up front would be
// almost entirely wasted work. Non-numeric members (Epic ships a few string and
// object siblings) carry no counter and are skipped outright.
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

// parseRawStats walks the "stats" object and folds its counters into overall
// and mode aggregates. It errors when no stats object can be located, rather
// than returning zeroed aggregates that a caller could mistake for a
// legitimately empty response -- note that a present-but-empty object is a real
// response shape (a brand new account) and is not an error.
func parseRawStats(data []byte) (modeAgg, [3]modeAgg, error) {
	var f statsFold
	if err := codec.ExtractEach(data, f.fold, statsPath); err != nil && !f.entered {
		// Nothing was read at all: the field is absent, or it is present as
		// something that is not an object (null, a string, an HTML error page
		// that never parsed). Both are the missing-stats case.
		return modeAgg{}, [3]modeAgg{}, errNoStatsObject
	}
	// A walk that failed after some members did reach the fold is a truncated
	// body -- rare transport damage -- and the counters already read are worth
	// more than discarding the response over the tail that never arrived.
	return f.overall, f.modes, nil
}
