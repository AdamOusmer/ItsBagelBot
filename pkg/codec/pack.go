// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package codec

import (
	"bytes"
	"fmt"
	"runtime"

	"github.com/klauspost/compress/zstd"
)

const (
	packMinSize    = 256
	packMinSavings = 10
	packWindow     = 128 << 10
	packMaxDecoded = 4 << 20
)

// Plain values must never start with the zstd magic or Unpack misreads them.
var zstdMagic = []byte{0x28, 0xB5, 0x2F, 0xFD}

var (
	packEncoder = mustPackEncoder()
	packDecoder = mustPackDecoder()
)

func mustPackEncoder() *zstd.Encoder {
	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithEncoderConcurrency(runtime.GOMAXPROCS(0)),
		zstd.WithWindowSize(packWindow),
		zstd.WithEncoderCRC(false),
	)
	if err != nil {
		panic(fmt.Sprintf("codec: zstd encoder: %v", err))
	}
	return enc
}

func mustPackDecoder() *zstd.Decoder {
	dec, err := zstd.NewReader(nil,
		zstd.WithDecoderConcurrency(0),
		zstd.WithDecoderLowmem(true),
		zstd.WithDecoderMaxWindow(packWindow),
		zstd.WithDecoderMaxMemory(packMaxDecoded),
	)
	if err != nil {
		panic(fmt.Sprintf("codec: zstd decoder: %v", err))
	}
	return dec
}

// Pack returns src itself when compression does not pay; otherwise a new slice.
func Pack(src []byte) []byte {
	if len(src) < packMinSize || len(src) > packMaxDecoded {
		return src
	}
	packed := packEncoder.EncodeAll(src, make([]byte, 0, len(src)/2))
	if len(packed)*100 > len(src)*(100-packMinSavings) {
		return src
	}
	return packed
}

func Unpack(src []byte) ([]byte, error) {
	if !bytes.HasPrefix(src, zstdMagic) {
		return src, nil
	}
	out, err := packDecoder.DecodeAll(src, nil)
	if err != nil {
		return nil, fmt.Errorf("codec: unpack: %w", err)
	}
	return out, nil
}
