// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"cmp"
	"encoding/base64"
	"mime"
	"strconv"
	"strings"
)

type vertexResponse struct {
	Candidates     []vertexCandidate `json:"candidates"`
	Usage          vertexUsage       `json:"usageMetadata"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}

type vertexCandidate struct {
	FinishReason string `json:"finishReason"`
	Content      struct {
		Parts []struct {
			InlineData vertexInlineData `json:"inlineData"`
		} `json:"parts"`
	} `json:"content"`
}

type vertexInlineData struct {
	MimeType vertexMediaType `json:"mimeType"`
	Data     string          `json:"data"`
}

type vertexMediaType string

func (r vertexResponse) speech() (speech, error) {
	if r.PromptFeedback.BlockReason != "" {
		return speech{}, errRefused
	}
	if len(r.Candidates) == 0 {
		return speech{}, errNoAudio
	}
	candidate := r.Candidates[0]
	data := candidate.audio()
	if data.Data == "" {
		return speech{}, candidate.emptyAudioError()
	}
	wav, duration, err := data.wav()
	if err != nil {
		return speech{}, err
	}
	return speech{wav: wav, durationMS: duration, usage: r.Usage.tokens()}, nil
}

func (c vertexCandidate) audio() vertexInlineData {
	for _, part := range c.Content.Parts {
		if part.InlineData.Data != "" && part.InlineData.MimeType.isAudio() {
			return part.InlineData
		}
	}
	return vertexInlineData{}
}

func (c vertexCandidate) emptyAudioError() error {
	if c.FinishReason == "" || c.FinishReason == "STOP" {
		return errNoAudio
	}
	return errRefused
}

func (d vertexInlineData) wav() ([]byte, int64, error) {
	if d.MimeType.isWAV() {
		return decodeWAV(d.Data)
	}
	rate, err := d.MimeType.sampleRate()
	if err != nil {
		return nil, 0, err
	}
	pcm, err := base64.StdEncoding.DecodeString(d.Data)
	if err != nil || !hasPCMFrames(pcm) {
		return nil, 0, errInvalidAudio
	}
	wav, duration := wrapPCM(pcm, rate)
	return wav, duration, nil
}

func (m vertexMediaType) isAudio() bool {
	return m == "" || strings.HasPrefix(strings.ToLower(string(m)), "audio/")
}

func (m vertexMediaType) isWAV() bool {
	typ, _, err := mime.ParseMediaType(string(m))
	if err != nil {
		return false
	}
	switch typ {
	case "audio/wav", "audio/x-wav", "audio/wave":
		return true
	}
	return false
}

func (m vertexMediaType) sampleRate() (uint32, error) {
	if m == "" {
		return wavSampleRate, nil
	}
	typ, params, err := mime.ParseMediaType(string(m))
	if err != nil {
		return 0, errInvalidAudio
	}
	switch typ {
	case "audio/l16", "audio/pcm":
		return pcmSampleRate(params)
	default:
		return 0, errInvalidAudio
	}
}

func pcmSampleRate(params map[string]string) (uint32, error) {
	if codec := params["codec"]; codec != "" && !strings.EqualFold(codec, "pcm") {
		return 0, errInvalidAudio
	}
	if params["rate"] == "" {
		return wavSampleRate, nil
	}
	rate, err := strconv.ParseUint(params["rate"], 10, 31)
	if err != nil || rate == 0 {
		return 0, errInvalidAudio
	}
	return uint32(rate), nil
}

type vertexUsage struct {
	Prompt     int64 `json:"promptTokenCount"`
	Candidates int64 `json:"candidatesTokenCount"`
	Total      int64 `json:"totalTokenCount"`
	Details    []struct {
		Modality string `json:"modality"`
		Count    int64  `json:"tokenCount"`
	} `json:"candidatesTokensDetails"`
}

func (u vertexUsage) tokens() tokenUsage {
	candidates := max(0, u.Candidates)
	audio := candidates
	if len(u.Details) != 0 {
		audio = u.audioTokens()
	}
	total := cmp.Or(max(0, u.Total), max(0, u.Prompt)+candidates)
	return tokenUsage{audio: audio, total: total}
}

func (u vertexUsage) audioTokens() int64 {
	var audio int64
	for _, detail := range u.Details {
		if detail.Modality == "AUDIO" {
			audio += max(0, detail.Count)
		}
	}
	return audio
}
