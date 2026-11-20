// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"slices"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"
)

const wavMIME = "audio/wav"

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
}

type speechVoice struct {
	Voice string `json:"voice"`
}

func interactionFor(req gossiprpc.Request) interaction {
	content := textContent{Type: "text", Text: req.Text}
	if req.Style != "" {
		content.Annotations = []speechMetadata{{Type: "speech_metadata", Style: req.Style}}
	}
	return interaction{
		Model:            req.Model,
		Input:            []inputStep{{Type: "user_input", Content: []textContent{content}}},
		ResponseFormat:   responseFormat{Type: "audio", MimeType: wavMIME},
		GenerationConfig: generationConfig{SpeechConfig: []speechVoice{{Voice: req.Voice}}},
	}
}

type interactionResponse struct {
	Steps []responseStep `json:"steps"`
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

func (s synthesizer) fetch(ctx context.Context, req gossiprpc.Request, billed int) gossiprpc.TTSSynthesizeReply {
	body, err := codec.Marshal(interactionFor(req))
	if err != nil {
		return gossiprpc.TTSSynthesizeReply{Error: "internal error"}
	}
	var result interactionResponse
	err = s.client.Do(ctx, core.Request{
		Method:      http.MethodPost,
		Path:        "/v1beta/interactions",
		Body:        body,
		NoRedirects: true,
	}, &result)
	if err != nil {
		return s.failure(ctx, err)
	}
	return result.reply(ctx, req.Model, billed)
}

func (r interactionResponse) reply(ctx context.Context, model gossiprpc.TTSModel, billed int) gossiprpc.TTSSynthesizeReply {
	data := r.audio()
	if data == "" {
		err := errors.New("speech provider returned no audio")
		core.RecordFetchError(ctx, err)
		return gossiprpc.TTSSynthesizeReply{Error: err.Error()}
	}
	audio, duration, err := decodeWAV(data)
	if err != nil {
		core.RecordFetchError(ctx, err)
		return gossiprpc.TTSSynthesizeReply{Error: "speech provider returned invalid audio"}
	}
	return gossiprpc.TTSSynthesizeReply{
		Audio:            audio,
		MimeType:         wavMIME,
		DurationMS:       duration,
		BilledCharacters: billed,
		Model:            model,
	}
}

func decodeWAV(data string) ([]byte, int64, error) {
	audio, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, 0, err
	}
	duration, err := wavDuration(audio)
	return audio, duration, err
}
