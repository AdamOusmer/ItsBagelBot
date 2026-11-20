// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"encoding/binary"
	"errors"
)

const (
	wavSampleRate = 24000
	wavFrameBytes = 2
	wavByteRate   = wavSampleRate * wavFrameBytes
)

var errInvalidWAV = errors.New("invalid PCM WAV")

type wavFormat struct {
	Encoding   uint16
	Channels   uint16
	SampleRate uint32
	ByteRate   uint32
	FrameBytes uint16
	SampleBits uint16
}

func wavDuration(wav []byte) (int64, error) {
	if !isRIFFWAV(wav) {
		return 0, errInvalidWAV
	}
	var format, pcm []byte
	for chunks := wav[12:]; len(chunks) > 0; {
		if len(chunks) < 8 {
			return 0, errInvalidWAV
		}
		size := uint64(binary.LittleEndian.Uint32(chunks[4:8]))
		padded := size + size%2
		if padded > uint64(len(chunks)-8) {
			return 0, errInvalidWAV
		}
		switch string(chunks[:4]) {
		case "fmt ":
			format = chunks[8 : 8+size]
		case "data":
			pcm = chunks[8 : 8+size]
		}
		chunks = chunks[8+padded:]
	}
	if !isGeminiPCM(format, pcm) {
		return 0, errInvalidWAV
	}
	return int64(len(pcm)) * 1000 / wavByteRate, nil
}

func isRIFFWAV(wav []byte) bool {
	if len(wav) < 12 {
		return false
	}
	return string(wav[:4]) == "RIFF" && string(wav[8:12]) == "WAVE" &&
		uint64(binary.LittleEndian.Uint32(wav[4:8])) == uint64(len(wav)-8)
}

func isGeminiPCM(format, pcm []byte) bool {
	var decoded wavFormat
	_, err := binary.Decode(format, binary.LittleEndian, &decoded)
	want := wavFormat{Encoding: 1, Channels: 1, SampleRate: wavSampleRate, ByteRate: wavByteRate, FrameBytes: wavFrameBytes, SampleBits: 16}
	return err == nil && decoded == want && len(pcm) > 0 && len(pcm)%wavFrameBytes == 0
}
