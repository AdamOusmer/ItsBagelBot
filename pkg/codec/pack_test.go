// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package codec_test

import (
	"ItsBagelBot/pkg/codec"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func leaderboardEnvelope(users int) []byte {
	var b strings.Builder
	b.WriteString(`{"v":[`)
	for i := 0; i < users; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"nickname":"Player%d_mc","seasonResult":{"eloRate":%d,"phasePoint":%d,"predPhasePoint":null}}`, i*7919%10007, 2400-i*11, 1200-i*5)
	}
	b.WriteString(`],"f":1790000000000}`)
	return []byte(b.String())
}

func TestPackRoundTripCompressesRealisticPayload(t *testing.T) {
	src := leaderboardEnvelope(100)

	packed := codec.Pack(src)
	require.Less(t, len(packed), len(src)*9/10)

	got, err := codec.Unpack(packed)
	require.NoError(t, err)
	require.Equal(t, src, got)
}

func TestPackLeavesSmallInputUntouched(t *testing.T) {
	src := []byte(`{"v":{"nickname":"x"},"f":1}`)
	require.Equal(t, src, codec.Pack(src))
}

func TestPackLeavesIncompressibleInputUntouched(t *testing.T) {
	src := make([]byte, 1024)
	state := uint32(1)
	for i := range src {
		state = state*1664525 + 1013904223
		src[i] = byte(state >> 24)
	}
	require.Equal(t, src, codec.Pack(src))
}

func TestUnpackPassesLegacyPlainJSONThrough(t *testing.T) {
	src := leaderboardEnvelope(100)
	got, err := codec.Unpack(src)
	require.NoError(t, err)
	require.Equal(t, src, got)
}

func TestUnpackRejectsOversizedOutput(t *testing.T) {
	enc, err := zstd.NewWriter(nil, zstd.WithWindowSize(128<<10))
	require.NoError(t, err)
	bomb := enc.EncodeAll(bytes.Repeat([]byte("a"), 8<<20), nil)

	_, err = codec.Unpack(bomb)
	require.ErrorIs(t, err, zstd.ErrDecoderSizeExceeded)
}

func TestPackLeavesInputAboveDecodeCapUntouched(t *testing.T) {
	src := bytes.Repeat([]byte("a"), 4<<20+1)
	require.Equal(t, len(src), len(codec.Pack(src)))
	require.Equal(t, src, codec.Pack(src))
}

func TestUnpackRejectsCorruptFrame(t *testing.T) {
	packed := codec.Pack(leaderboardEnvelope(100))
	_, err := codec.Unpack(packed[:len(packed)/2])
	require.Error(t, err)
}

func BenchmarkPack(b *testing.B) {
	src := leaderboardEnvelope(30)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		codec.Pack(src)
	}
	b.ReportMetric(float64(len(codec.Pack(src)))/float64(len(src)), "ratio")
}

func BenchmarkUnpack(b *testing.B) {
	src := leaderboardEnvelope(30)
	packed := codec.Pack(src)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := codec.Unpack(packed); err != nil {
			b.Fatal(err)
		}
	}
}
