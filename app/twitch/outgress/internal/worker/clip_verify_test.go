// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	clipFoundBody = `{"data":[{"id":"AbCdEf"}]}`
	clipEmptyBody = `{"data":[]}`
)

func TestClipConfirmedAbsent(t *testing.T) {
	tests := []struct {
		name      string
		responses []scriptedResponse
		want      bool
		wantPolls int
	}{
		{"confirms a clip absent twice", []scriptedResponse{{status: 200, body: clipEmptyBody}, {status: 200, body: clipEmptyBody}}, true, 2},
		{"stays silent on a late publish at the recheck", []scriptedResponse{{status: 200, body: clipEmptyBody}, {status: 200, body: clipFoundBody}}, false, 2},
		{"stops when the clip is found on the first poll", []scriptedResponse{{status: 200, body: clipFoundBody}}, false, 1},
		{"treats a non-200 as indeterminate", []scriptedResponse{{status: http.StatusServiceUnavailable}}, false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &scriptedTransport{responses: tt.responses}
			w := pipelineWorker(t, rt)

			got := w.clipConfirmedAbsent(context.Background(), clipProbe{broadcasterID: "123", clipID: "AbCdEf"}, 0, 0)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantPolls, rt.callCount())
		})
	}
}

func TestClipConfirmedAbsentCanceledContextStaysSilent(t *testing.T) {
	rt := &scriptedTransport{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := pipelineWorker(t, rt).clipConfirmedAbsent(ctx, clipProbe{broadcasterID: "123", clipID: "AbCdEf"}, clipVerifyDelay, clipVerifyRecheck)

	assert.False(t, got)
	assert.Zero(t, rt.callCount())
}

func TestClipFailedText(t *testing.T) {
	if got := clipFailedText("viewer"); !strings.HasPrefix(got, "@viewer ") {
		t.Errorf("clipFailedText with clipper = %q, want @viewer mention", got)
	}
	if got := clipFailedText(""); !strings.HasPrefix(got, "Heads up: ") {
		t.Errorf("clipFailedText without clipper = %q, want generic line", got)
	}
}
