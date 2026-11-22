// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"encoding/binary"
)

type chunkID string

type riffForm string

const (
	formWebP riffForm = "WEBP"
	formWave riffForm = "WAVE"
)

type riffChunk struct {
	id   chunkID
	body blob
}

type riffChunks []riffChunk

func parseRIFF(data blob, form riffForm) (riffChunks, error) {
	if len(data) < 12 {
		return nil, fail(CodeCorrupt)
	}
	if string(data[:4]) != "RIFF" || riffForm(data[8:12]) != form {
		return nil, fail(CodeCorrupt)
	}
	total := offset(binary.LittleEndian.Uint32(data[4:])) + 8
	if total < 12 || total > data.end() {
		return nil, fail(CodeCorrupt)
	}
	var chunks riffChunks
	for rest := data[12:total]; len(rest) > 0; {
		chunk, next, err := rest.riffNext()
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
		rest = next
	}
	return chunks, nil
}

func (d blob) riffNext() (riffChunk, blob, error) {
	if len(d) < 8 {
		return riffChunk{}, nil, fail(CodeCorrupt)
	}
	size := offset(binary.LittleEndian.Uint32(d[4:]))
	end := 8 + size
	if end > d.end() {
		return riffChunk{}, nil, fail(CodeCorrupt)
	}
	return riffChunk{id: chunkID(d[:4]), body: d[8:end]}, d[min(end+size&1, d.end()):], nil
}

func writeRIFF(form riffForm, chunks riffChunks) blob {
	out := append(blob("RIFF\x00\x00\x00\x00"), string(form)...)
	for _, c := range chunks {
		out = append(out, string(c.id)...)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(c.body)))
		out = append(out, c.body...)
		if len(c.body)&1 == 1 {
			out = append(out, 0)
		}
	}
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	return out
}

func (c riffChunks) require(id chunkID) (riffChunk, error) {
	for _, chunk := range c {
		if chunk.id == id {
			return chunk, nil
		}
	}
	return riffChunk{}, fail(CodeCorrupt)
}
