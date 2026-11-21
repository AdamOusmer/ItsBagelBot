// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"cmp"
	"context"
	"encoding/base64"
	"net/http"
	"slices"
	"strings"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"
)

type geminiBackend struct {
	client      *core.HTTPClient
	temperature float64
}

type interaction struct {
	Model            gossiprpc.TTSModel `json:"model"`
	Input            []inputStep        `json:"input"`
	ResponseFormat   responseFormat     `json:"response_format"`
	GenerationConfig generationConfig   `json:"generation_config"`
}

type inputStep struct {
	Type    string        `json:"type"`
	Content []textContent `json:"content"`
}

type textContent struct {
	Type        string           `json:"type"`
	Text        string           `json:"text"`
	Annotations []speechMetadata `json:"annotations,omitempty"`
}

type speechMetadata struct {
	Type  string `json:"type"`
	Style string `json:"style"`
}

type responseFormat struct {
	Type     string `json:"type"`
	MimeType string `json:"mime_type"`
}

type generationConfig struct {
	SpeechConfig []speechVoice `json:"speech_config"`
	Temperature  float64       `json:"temperature"`
}

type speechVoice struct {
	Voice string `json:"voice"`
}

func (g geminiBackend) interactionFor(req gossiprpc.Request) interaction {
	content := textContent{Type: "text", Text: req.Text}
	if req.Style != "" {
		content.Annotations = []speechMetadata{{Type: "speech_metadata", Style: req.Style}}
	}
	return interaction{
		Model:            req.Model,
		Input:            []inputStep{{Type: "user_input", Content: []textContent{content}}},
		ResponseFormat:   responseFormat{Type: "audio", MimeType: wavMIME},
		GenerationConfig: generationConfig{SpeechConfig: []speechVoice{{Voice: req.Voice}}, Temperature: g.temperature},
	}
}

type interactionResponse struct {
	Steps         []responseStep   `json:"steps"`
	Usage         codec.RawMessage `json:"usage"`
	UsageMetadata codec.RawMessage `json:"usage_metadata"`
	UsageCamel    codec.RawMessage `json:"usageMetadata"`
}

type responseStep struct {
	Type    string          `json:"type"`
	Content []outputContent `json:"content"`
}

type outputContent struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

func (r interactionResponse) audio() string {
	for _, step := range slices.Backward(r.Steps) {
		if step.Type != "model_output" {
			continue
		}
		if data := step.audio(); data != "" {
			return data
		}
	}
	return ""
}

func (s responseStep) audio() string {
	for _, content := range slices.Backward(s.Content) {
		if content.Type == "audio" && content.Data != "" {
			return content.Data
		}
	}
	return ""
}

func (g geminiBackend) generate(ctx context.Context, req gossiprpc.Request) (speech, error) {
	body, err := codec.Marshal(g.interactionFor(req))
	if err != nil {
		return speech{}, errInternal
	}
	var result interactionResponse
	err = g.client.Do(ctx, core.Request{
		Method:      http.MethodPost,
		Path:        "/v1beta/interactions",
		Body:        body,
		NoRedirects: true,
		DecodeError: decodeGeminiError,
	}, &result)
	if err != nil {
		return speech{}, err
	}
	return result.speech()
}

func (r interactionResponse) speech() (speech, error) {
	data := r.audio()
	if data == "" {
		return speech{}, errNoAudio
	}
	audio, duration, err := decodeWAV(data)
	if err != nil {
		return speech{}, errInvalidAudio
	}
	return speech{wav: audio, durationMS: duration, usage: r.tokenUsage()}, nil
}

func (r interactionResponse) tokenUsage() tokenUsage {
	var usage tokenUsage
	for _, raw := range []codec.RawMessage{r.Usage, r.UsageMetadata, r.UsageCamel} {
		var fields map[string]codec.RawMessage
		if len(raw) == 0 || codec.Unmarshal(raw, &fields) != nil {
			continue
		}
		usage.audio = cmp.Or(usage.audio, audioTokenCount(fields))
		usage.total = cmp.Or(usage.total, tokenCount(fields, "total_tokens", "total_token_count", "totalTokenCount"))
	}
	return usage
}

func audioTokenCount(fields map[string]codec.RawMessage) int64 {
	var modalities []struct {
		Modality string `json:"modality"`
		Tokens   int64  `json:"tokens"`
	}
	_ = codec.Unmarshal(fields["output_tokens_by_modality"], &modalities)
	for _, entry := range modalities {
		if strings.EqualFold(entry.Modality, "audio") {
			return max(0, entry.Tokens)
		}
	}
	return tokenCount(fields, "audio_tokens", "total_output_tokens", "output_tokens", "candidates_token_count", "candidatesTokenCount")
}

func tokenCount(fields map[string]codec.RawMessage, names ...string) int64 {
	for _, name := range names {
		var count int64
		if err := codec.Unmarshal(fields[name], &count); err == nil && count > 0 {
			return count
		}
	}
	return 0
}

func decodeWAV(data string) ([]byte, int64, error) {
	audio, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, 0, err
	}
	duration, err := wavDuration(audio)
	return audio, duration, err
}
