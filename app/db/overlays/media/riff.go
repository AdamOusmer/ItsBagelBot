// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"encoding/binary"
)

type riffChunk struct {
	id   string
	body []byte
}

func parseRIFF(data []byte, form string) ([]riffChunk, error) {
	if len(data) < 12 {
		return nil, fail(CodeCorrupt)
	}
	if string(data[:4]) != "RIFF" || string(data[8:12]) != form {
		return nil, fail(CodeCorrupt)
	}
	total := int(binary.LittleEndian.Uint32(data[4:])) + 8
	if total < 12 || total > len(data) {
		return nil, fail(CodeCorrupt)
	}
	var chunks []riffChunk
	for rest := data[12:total]; len(rest) > 0; {
		chunk, next, err := riffNext(rest)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
		rest = next
	}
	return chunks, nil
}

func riffNext(data []byte) (riffChunk, []byte, error) {
	if len(data) < 8 {
		return riffChunk{}, nil, fail(CodeCorrupt)
	}
	size := int(binary.LittleEndian.Uint32(data[4:]))
	end := 8 + size
	if end > len(data) {
		return riffChunk{}, nil, fail(CodeCorrupt)
	}
	return riffChunk{id: string(data[:4]), body: data[8:end]}, data[min(end+size&1, len(data)):], nil
}

func writeRIFF(form string, chunks []riffChunk) []byte {
	out := append([]byte("RIFF\x00\x00\x00\x00"), form...)
	for _, c := range chunks {
		out = append(out, c.id...)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(c.body)))
		out = append(out, c.body...)
		if len(c.body)&1 == 1 {
			out = append(out, 0)
		}
	}
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	return out
}

func requireChunk(chunks []riffChunk, id string) (riffChunk, error) {
	for _, c := range chunks {
		if c.id == id {
			return c, nil
		}
	}
	return riffChunk{}, fail(CodeCorrupt)
}
