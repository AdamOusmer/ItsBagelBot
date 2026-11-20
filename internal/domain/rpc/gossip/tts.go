// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gossiprpc

type TTSModel string

const (
	TTSFlash     TTSModel = "gemini-3.8-flash-tts"
	TTSFlashLite TTSModel = "gemini-3.8-flash-lite-tts"
)

type TTSSynthesizeReply struct {
	Audio            []byte   `json:"audio,omitempty"`
	MimeType         string   `json:"mime_type,omitempty"`
	DurationMS       int64    `json:"duration_ms"`
	BilledCharacters int      `json:"billed_characters"`
	Model            TTSModel `json:"model,omitempty"`
	Error            string   `json:"error,omitempty"`
	RateLimited      bool     `json:"rate_limited,omitempty"`
	RetryAfterMS     int64    `json:"retry_after_ms,omitempty"`
}

type TTSVoicesReply struct {
	Voices []string `json:"voices"`
	Model  TTSModel `json:"model"`
}
