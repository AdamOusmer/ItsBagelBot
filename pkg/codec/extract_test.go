// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package codec_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const doc = `{
	"account_id":"abc-123",
	"level":74,
	"ratio":1.75,
	"active":true,
	"missing_child":null,
	"name":"say \"hi\"",
	"profile":{"region":"eu","wins":12},
	"modes":["solo","duo","squad"],
	"stats":{"br_kills_solo":41,"br_wins_solo":3,"br_kills_duo":58,"other":9}
}`

type extracted func(data []byte, path codec.Path) (string, error)

func asString(data []byte, path codec.Path) (string, error) {
	return codec.ExtractString(data, path)
}

func asInt(data []byte, path codec.Path) (string, error) {
	v, err := codec.ExtractInt(data, path)
	return strconv.FormatInt(v, 10), err
}

func asFloat(data []byte, path codec.Path) (string, error) {
	v, err := codec.ExtractFloat(data, path)
	return strconv.FormatFloat(v, 'g', -1, 64), err
}

func asBool(data []byte, path codec.Path) (string, error) {
	v, err := codec.ExtractBool(data, path)
	return strconv.FormatBool(v), err
}

func TestExtractScalars(t *testing.T) {
	tests := []struct {
		name string
		get  extracted
		path codec.Path
		want string
	}{
		{"extracts a string", asString, codec.Path{"account_id"}, "abc-123"},
		{"extracts an int", asInt, codec.Path{"level"}, "74"},
		{"extracts a float", asFloat, codec.Path{"ratio"}, "1.75"},
		{"extracts a bool", asBool, codec.Path{"active"}, "true"},
		{"unescapes a string", asString, codec.Path{"name"}, `say "hi"`},
		{"extracts a nested string", asString, codec.Path{"profile", "region"}, "eu"},
		{"extracts a nested int", asInt, codec.Path{"profile", "wins"}, "12"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.get([]byte(doc), tc.path)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestExtractValueReportsKinds(t *testing.T) {
	tests := []struct {
		name string
		path codec.Path
		want codec.Kind
	}{
		{"reports a string", codec.Path{"account_id"}, codec.KindString},
		{"reports a number", codec.Path{"level"}, codec.KindNumber},
		{"reports a bool", codec.Path{"active"}, codec.KindBool},
		{"reports an object", codec.Path{"profile"}, codec.KindObject},
		{"reports an array", codec.Path{"modes"}, codec.KindArray},
		{"reports null as a present value, not a missing key", codec.Path{"missing_child"}, codec.KindNull},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, kind, err := codec.ExtractValue([]byte(doc), tc.path)

			require.NoError(t, err)
			assert.Equal(t, tc.want, kind)
		})
	}

	t.Run("returns an object value that decodes as JSON", func(t *testing.T) {
		raw, _, err := codec.ExtractValue([]byte(doc), codec.Path{"profile"})
		require.NoError(t, err)

		var profile struct {
			Region string `json:"region"`
			Wins   int    `json:"wins"`
		}
		require.NoError(t, codec.Unmarshal(raw, &profile))
		assert.Equal(t, "eu", profile.Region)
		assert.Equal(t, 12, profile.Wins)
	})
}

func TestExtractRejectsMissingPathsAndMalformedDocuments(t *testing.T) {
	t.Run("reports ErrNotFound naming the missing path", func(t *testing.T) {
		_, err := codec.ExtractString([]byte(doc), codec.Path{"profile", "nope"})

		require.ErrorIs(t, err, codec.ErrNotFound)
		assert.ErrorContains(t, err, "nope")
	})
	t.Run("reports ErrNotFound when iterating a missing object", func(t *testing.T) {
		err := codec.ExtractEach([]byte(doc), func(_, _ []byte, _ codec.Kind) error { return nil }, codec.Path{"nope"})

		assert.ErrorIs(t, err, codec.ErrNotFound)
	})
	t.Run("rejects a malformed document", func(t *testing.T) {
		_, err := codec.ExtractString([]byte(`{"a":`), codec.Path{"a"})

		assert.Error(t, err)
	})
}

func TestExtractEachWalksObjectMembers(t *testing.T) {
	t.Run("aggregates matching members", func(t *testing.T) {
		var total int64
		var seen int
		err := codec.ExtractEach([]byte(doc), func(key, value []byte, kind codec.Kind) error {
			seen++
			if kind != codec.KindNumber || !strings.HasPrefix(string(key), "br_kills_") {
				return nil
			}
			n, err := codec.ParseInt(value)
			total += n
			return err
		}, codec.Path{"stats"})

		require.NoError(t, err)
		assert.Equal(t, 4, seen)
		assert.Equal(t, int64(99), total)
	})
	t.Run("stops at the callback error and returns it unchanged", func(t *testing.T) {
		stop := errors.New("stop")
		var visited int
		err := codec.ExtractEach([]byte(doc), func(_, _ []byte, _ codec.Kind) error {
			visited++
			return stop
		}, codec.Path{"stats"})

		require.ErrorIs(t, err, stop)
		assert.Equal(t, 1, visited)
	})
}

func TestExtractArrayVisitsEveryElement(t *testing.T) {
	var got []string
	err := codec.ExtractArray([]byte(doc), func(value []byte, kind codec.Kind) error {
		assert.Equal(t, codec.KindString, kind)
		s, err := codec.ParseString(value)
		got = append(got, s)
		return err
	}, codec.Path{"modes"})

	require.NoError(t, err)
	assert.Equal(t, []string{"solo", "duo", "squad"}, got)
}

var sinkInt int64

func TestExtractEachAllocationIsFlat(t *testing.T) {
	scan := func(data []byte) float64 {
		return testing.AllocsPerRun(50, func() {
			var total int64
			_ = codec.ExtractEach(data, func(key, value []byte, kind codec.Kind) error {
				if kind == codec.KindNumber && strings.HasPrefix(string(key), "br_kills_") {
					n, _ := codec.ParseInt(value)
					total += n
				}
				return nil
			}, codec.Path{"stats"})
			sinkInt = total
		})
	}

	narrow := scan(statsDoc(5))
	wide := scan(statsDoc(5000))

	assert.Equal(t, narrow, wide, "allocations scale with document size")
	assert.LessOrEqual(t, wide, 1.0, "ExtractEach allocations per call")
}

func statsDoc(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"stats":{`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"br_kills_m`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`":`)
		b.WriteString(strconv.Itoa(i))
	}
	b.WriteString(`}}`)
	return []byte(b.String())
}
