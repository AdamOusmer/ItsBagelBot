package fortnite

import (
	"bytes"
	"errors"
	"math"
	"strconv"
)

// errNoStatsObject is returned when the response body has no locatable "stats"
// object (empty body, an error page, or a payload missing the field). Callers
// must treat this as a hard failure rather than serving zeroed aggregates.
var errNoStatsObject = errors.New("fortnite: stats object missing from response")

// rawStatsResponse is the /api/v2/stats/{accountId} body: Epic's raw stats-v2
// counters. It implements json.Unmarshaler with a zero-allocation byte scanner
// that folds the raw counter blob directly into overall and per-mode aggregates
// on decode without allocating maps, strings, or intermediate heap objects.
type rawStatsResponse struct {
	Overall modeAgg
	Modes   [3]modeAgg
}

// UnmarshalJSON scans the raw stats JSON directly without allocating maps,
// strings, or intermediate heap objects.
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

// statScanner encapsulates byte scanning state.
type statScanner struct {
	data []byte
	i    int
}

func isWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func (s *statScanner) skipWhitespace() {
	for s.i < len(s.data) && (isWhitespace(s.data[s.i]) || s.data[s.i] == ',') {
		s.i++
	}
}

func (s *statScanner) skipColon() {
	for s.i < len(s.data) && (isWhitespace(s.data[s.i]) || s.data[s.i] == ':') {
		s.i++
	}
}

func (s *statScanner) readKey() ([]byte, bool) {
	if s.i >= len(s.data) || s.data[s.i] != '"' {
		return nil, false
	}
	s.i++ // past opening '"'
	start := s.i
	for s.i < len(s.data) && s.data[s.i] != '"' {
		if s.data[s.i] == '\\' && s.i+1 < len(s.data) {
			s.i++
		}
		s.i++
	}
	if s.i >= len(s.data) {
		return nil, false
	}
	end := s.i
	s.i++ // past closing '"'
	return s.data[start:end], true
}

func (s *statScanner) readNumber() int64 {
	valStart := s.i
	for s.i < len(s.data) && s.data[s.i] != ',' && s.data[s.i] != '}' && !isWhitespace(s.data[s.i]) {
		s.i++
	}
	if s.i == valStart {
		return 0
	}
	return parseNumberBytes(s.data[valStart:s.i])
}

// maxInt64Div10 bounds the fast accumulation path in parseNumberBytes: once val
// exceeds it, one more digit could overflow int64.
const maxInt64Div10 = math.MaxInt64 / 10

// willOverflowInt64 reports whether accumulating digit c into val would overflow
// int64 accumulation in parseNumberBytes.
func willOverflowInt64(val int64, c byte) bool {
	return val > maxInt64Div10 || (val == maxInt64Div10 && c > '7')
}

// parseNumberFallback handles tokens the fast integer path can't: floats,
// scientific notation, and integers too large for int64 accumulation.
func parseNumberFallback(raw []byte) int64 {
	f, err := strconv.ParseFloat(string(raw), 64)
	if err != nil {
		return 0
	}
	return int64(f)
}

// parseNumberBytes parses a numeric token, defaulting to fast integer accumulation
// and falling back to ParseFloat for scientific notation, floats, or overflow.
func parseNumberBytes(raw []byte) int64 {
	var val int64
	var isNeg bool
	idx := 0
	if raw[0] == '-' {
		isNeg = true
		idx++
	}
	for idx < len(raw) {
		c := raw[idx]
		if c < '0' || c > '9' {
			return parseNumberFallback(raw)
		}
		if willOverflowInt64(val, c) {
			return parseNumberFallback(raw)
		}
		val = val*10 + int64(c-'0')
		idx++
	}
	if isNeg {
		return -val
	}
	return val
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

// foldStatEntry matches the stat key and accumulates the value into overall and modes.
func foldStatEntry(overall *modeAgg, modes *[3]modeAgg, key []byte, val int64) {
	metric, pl, ok := matchStatKeyBytes(key)
	if !ok {
		return
	}
	addMetric(overall, metric, val)
	if idx, ok := coreModeIndexBytes(pl); ok {
		addMetric(&modes[idx], metric, val)
	}
}

// findStatsStart locates the opening '{' of the "stats" object. Returns -1 if not found.
func findStatsStart(data []byte) int {
	idx := bytes.Index(data, []byte(`"stats"`))
	if idx == -1 {
		return -1
	}
	i := idx + 7
	for i < len(data) && data[i] != '{' {
		i++
	}
	if i >= len(data) {
		return -1
	}
	return i + 1
}

// skipString advances past a JSON string literal (opening quote already
// current), honoring backslash escapes so an escaped quote doesn't end it early.
func (s *statScanner) skipString() {
	s.i++ // past opening '"'
	for s.i < len(s.data) {
		c := s.data[s.i]
		if c == '\\' && s.i+1 < len(s.data) {
			s.i += 2
			continue
		}
		if c == '"' {
			s.i++
			return
		}
		s.i++
	}
}

// skipContainer advances past a balanced object or array value, tracking
// nesting depth so an inner "}"/"]" doesn't end the scan early. String
// literals are consumed whole so braces/brackets inside them don't count.
func (s *statScanner) skipContainer() {
	depth := 0
	for s.i < len(s.data) {
		switch c := s.data[s.i]; {
		case c == '"':
			s.skipString()
			continue
		case c == '{' || c == '[':
			depth++
		case c == '}' || c == ']':
			depth--
		}
		s.i++
		if depth == 0 {
			return
		}
	}
}

// readValue consumes one JSON value -- number, string, object, or array --
// keeping the scanner synchronized with sibling keys either way. Only numeric
// tokens are meaningful stat values, so strings/objects/arrays fold to 0.
func (s *statScanner) readValue() int64 {
	if s.i >= len(s.data) {
		return 0
	}
	switch s.data[s.i] {
	case '"':
		s.skipString()
		return 0
	case '{', '[':
		s.skipContainer()
		return 0
	default:
		return s.readNumber()
	}
}

// scanEntry parses one stat key/value pair. Returns true to continue scanning.
func (s *statScanner) scanEntry(overall *modeAgg, modes *[3]modeAgg) bool {
	s.skipWhitespace()
	if s.i >= len(s.data) || s.data[s.i] == '}' {
		return false
	}
	key, ok := s.readKey()
	if !ok {
		s.i++
		return true
	}
	s.skipColon()
	val := s.readValue()
	foldStatEntry(overall, modes, key, val)
	return true
}

// parseRawStats scans the stats JSON body directly and folds the counters
// into overall and mode aggregates with zero memory allocations. It errors
// when no "stats" object can be located, rather than returning zeroed
// aggregates that a caller could mistake for a legitimately empty response.
func parseRawStats(data []byte) (overall modeAgg, modes [3]modeAgg, err error) {
	start := findStatsStart(data)
	if start == -1 {
		return overall, modes, errNoStatsObject
	}
	s := statScanner{data: data, i: start}
	for s.scanEntry(&overall, &modes) {
	}
	return overall, modes, nil
}
