// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func TestStreamStatusFailureAcksPermanentRejections(t *testing.T) {
	w := &Worker{log: zap.NewNop()}

	tests := []struct {
		name     string
		err      error
		wantNack bool
	}{
		{name: "valkey server rejection", err: &valkey.ValkeyError{}},
		{name: "wrapped valkey server rejection", err: fmt.Errorf("live write: %w", &valkey.ValkeyError{})},
		{name: "twitch 4xx", err: &twitch.StatusError{Status: http.StatusBadRequest}},
		{name: "valkey connection timeout", err: context.DeadlineExceeded, wantNack: true},
		{name: "twitch rate limit", err: &twitch.StatusError{Status: http.StatusTooManyRequests}, wantNack: true},
		{name: "twitch 5xx", err: &twitch.StatusError{Status: http.StatusBadGateway}, wantNack: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := w.streamStatusFailure(context.Background(), "1234", tt.err)
			if (got != nil) != tt.wantNack {
				t.Fatalf("streamStatusFailure(%v) = %v, want nack=%v", tt.err, got, tt.wantNack)
			}
		})
	}
}
