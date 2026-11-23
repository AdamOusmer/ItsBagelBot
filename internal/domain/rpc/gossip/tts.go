// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gossiprpc

type TTSModel string

const (
	TTSFlash     TTSModel = "gemini-3.8-flash-tts"
	TTSFlashLite TTSModel = "gemini-3.8-flash-lite-tts"
)

type TTSSynthesizeReply struct {
	Audio            []byte       `json:"audio,omitempty"`
	MimeType         string       `json:"mime_type,omitempty"`
	DurationMS       int64        `json:"duration_ms"`
	BilledCharacters int          `json:"billed_characters"`
	StyleCharacters  int          `json:"style_characters"`
	AudioTokens      int64        `json:"audio_tokens"`
	TotalTokens      int64        `json:"total_tokens"`
	Model            TTSModel     `json:"model,omitempty"`
	Error            string       `json:"error,omitempty"`
	Reason           string       `json:"reason,omitempty"`
	RateLimited      bool         `json:"rate_limited,omitempty"`
	RetryAfterMS     int64        `json:"retry_after_ms,omitempty"`
	RefusalClass     RefusalClass `json:"refusal_class,omitempty"`
	Gate             string       `json:"gate,omitempty"`
}

type RefusalClass string

const (
	RefusalNone        RefusalClass = ""
	RefusalPolicy      RefusalClass = "policy"
	RefusalUnspecified RefusalClass = "policy_unspecified"
	RefusalUnknown     RefusalClass = "unknown"
	RefusalEmpty       RefusalClass = "empty"
	RefusalInvalid     RefusalClass = "invalid"
	RefusalLocal       RefusalClass = "local"
)

func (c RefusalClass) Strike() (weight int64, channelOnly bool) {
	switch c {
	case RefusalPolicy:
		return 1, false
	case RefusalUnspecified, RefusalUnknown:
		return 1, true
	}
	return 0, false
}

func (c RefusalClass) CountsTowardBreaker() bool {
	switch c {
	case RefusalPolicy, RefusalUnspecified, RefusalUnknown:
		return true
	}
	return false
}

type TTSVoicesReply struct {
	Voices []string `json:"voices"`
	Model  TTSModel `json:"model"`
}
