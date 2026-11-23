// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"encoding/binary"
	"errors"
)

const (
	wavMIME       = "audio/wav"
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

type wavPayload struct {
	format []byte
	pcm    []byte
}

func wavDuration(wav []byte) (int64, error) {
	if !isRIFFWAV(wav) {
		return 0, errInvalidWAV
	}
	var payload wavPayload
	var err error
	for chunks := wav[12:]; len(chunks) > 0 && err == nil; {
		chunks, err = payload.readChunk(chunks)
	}
	if err != nil || !isGeminiPCM(payload.format, payload.pcm) {
		return 0, errInvalidWAV
	}
	return int64(len(payload.pcm)) * 1000 / wavByteRate, nil
}

func (p *wavPayload) readChunk(chunks []byte) ([]byte, error) {
	if len(chunks) < 8 {
		return nil, errInvalidWAV
	}
	size := uint64(binary.LittleEndian.Uint32(chunks[4:8]))
	padded := size + size%2
	if padded > uint64(len(chunks)-8) {
		return nil, errInvalidWAV
	}
	switch string(chunks[:4]) {
	case "fmt ":
		p.format = chunks[8 : 8+size]
	case "data":
		p.pcm = chunks[8 : 8+size]
	}
	return chunks[8+padded:], nil
}

func isRIFFWAV(wav []byte) bool {
	if len(wav) < 12 {
		return false
	}
	return hasWAVSignature(wav) && uint64(binary.LittleEndian.Uint32(wav[4:8])) == uint64(len(wav)-8)
}

func hasWAVSignature(wav []byte) bool {
	return string(wav[:4]) == "RIFF" && string(wav[8:12]) == "WAVE"
}

func isGeminiPCM(format, pcm []byte) bool {
	var decoded wavFormat
	_, err := binary.Decode(format, binary.LittleEndian, &decoded)
	if err != nil || decoded != pcmFormat(wavSampleRate) {
		return false
	}
	return hasPCMFrames(pcm)
}

func hasPCMFrames(pcm []byte) bool {
	return len(pcm) > 0 && len(pcm)%wavFrameBytes == 0
}

func pcmFormat(rate uint32) wavFormat {
	return wavFormat{Encoding: 1, Channels: 1, SampleRate: rate, ByteRate: rate * wavFrameBytes, FrameBytes: wavFrameBytes, SampleBits: 16}
}

func wrapPCM(pcm []byte, rate uint32) ([]byte, int64) {
	const headerBytes = 44
	format := pcmFormat(rate)
	wav := make([]byte, headerBytes+len(pcm))
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	_, _ = binary.Encode(wav[20:36], binary.LittleEndian, format)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(len(pcm)))
	copy(wav[headerBytes:], pcm)
	return wav, int64(len(pcm)) * 1000 / int64(format.ByteRate)
}
