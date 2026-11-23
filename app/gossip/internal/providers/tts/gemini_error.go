// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"
)

var (
	errKeyRejected  = errors.New("speech provider rejected its API key")
	safetyBlockName = regexp.MustCompile(`(?i)(^|[^a-z0-9])(safety|prohibited[ _]content|blocklist)($|[^a-z0-9])`)
	apiKeyName      = regexp.MustCompile(`(?i)(^|[^a-z0-9])api[ _-]*keys?($|[^a-z0-9])`)
)

type geminiError struct {
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Details []struct {
		Type       string `json:"@type"`
		Reason     string `json:"reason"`
		RetryDelay string `json:"retryDelay"`
	} `json:"details"`
}

func decodeGeminiError(upstream *core.UpstreamError, body []byte) error {
	switch upstream.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: %w", errKeyRejected, upstream)
	case http.StatusBadRequest, http.StatusTooManyRequests:
		return readGeminiError(upstream, body)
	default:
		return upstream
	}
}

func readGeminiError(upstream *core.UpstreamError, body []byte) error {
	details := readGoogleError(body)
	if upstream.Status == http.StatusBadRequest && details.namesAPIKey() {
		return fmt.Errorf("%w: %w", errKeyRejected, upstream)
	}
	if upstream.Status == http.StatusTooManyRequests {
		upstream.RetryAfter = details.retryAfter(upstream.RetryAfter)
		return upstream
	}
	return details.blocked(upstream)
}

func (e geminiError) blocked(upstream *core.UpstreamError) error {
	if !e.namesSafetyBlock() {
		return upstream
	}
	return fmt.Errorf("%w: %w", refusal{class: gossiprpc.RefusalPolicy, raw: "HTTP_" + strconv.Itoa(upstream.Status)}, upstream)
}

func (e geminiError) namesSafetyBlock() bool {
	return e.names(safetyBlockName, e.Status+"\n"+e.Reason)
}

func readGoogleError(body []byte) geminiError {
	var envelope struct {
		Error geminiError `json:"error"`
	}
	_ = codec.Unmarshal(body, &envelope)
	return envelope.Error
}

func (e geminiError) namesAPIKey() bool {
	return e.names(apiKeyName, e.Status+"\n"+e.Reason+"\n"+e.Message)
}

func (e geminiError) names(pattern *regexp.Regexp, summary string) bool {
	if pattern.MatchString(summary) {
		return true
	}
	for _, detail := range e.Details {
		if pattern.MatchString(detail.Reason) {
			return true
		}
	}
	return false
}

func (e geminiError) retryAfter(fallback time.Duration) time.Duration {
	for _, detail := range e.Details {
		if detail.Type != "" && !strings.HasSuffix(detail.Type, "google.rpc.RetryInfo") {
			continue
		}
		if delay, err := time.ParseDuration(detail.RetryDelay); err == nil && delay > 0 {
			return delay
		}
	}
	return fallback
}
