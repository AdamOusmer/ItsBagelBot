package worker

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"ItsBagelBot/app/outgress/internal/twitch"

	"go.uber.org/zap"
)

// scriptedTransport serves one canned response per call, in order, and counts
// the calls so tests can assert how many polls actually went out.
type scriptedTransport struct {
	mu        sync.Mutex
	responses []scriptedResponse
	calls     int
}

type scriptedResponse struct {
	status int
	body   string
}

func (t *scriptedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.calls >= len(t.responses) {
		panic("scriptedTransport: more calls than scripted responses")
	}
	r := t.responses[t.calls]
	t.calls++
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(strings.NewReader(r.body)),
	}, nil
}

func (t *scriptedTransport) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func clipVerifyWorker(t *testing.T, rt http.RoundTripper) *Worker {
	t.Helper()
	tw := twitch.NewClient("test-client-id",
		twitch.NewStaticTokenSource("app-token"),
		twitch.NewStaticTokenSource("bot-token"), nil)
	tw.SetTransport(rt)
	return New(Config{Log: zap.NewNop(), Limiter: allowAll{}, Twitch: tw})
}

const (
	clipFoundBody = `{"data":[{"id":"AbCdEf"}]}`
	clipEmptyBody = `{"data":[]}`
)

func TestClipConfirmedAbsent(t *testing.T) {
	cases := []struct {
		name      string
		responses []scriptedResponse
		want      bool
		wantCalls int
	}{
		{
			name: "absent twice confirms",
			responses: []scriptedResponse{
				{http.StatusOK, clipEmptyBody},
				{http.StatusOK, clipEmptyBody},
			},
			want:      true,
			wantCalls: 2,
		},
		{
			name: "late publish on recheck stays silent",
			responses: []scriptedResponse{
				{http.StatusOK, clipEmptyBody},
				{http.StatusOK, clipFoundBody},
			},
			want:      false,
			wantCalls: 2,
		},
		{
			name:      "found on first poll stops",
			responses: []scriptedResponse{{http.StatusOK, clipFoundBody}},
			want:      false,
			wantCalls: 1,
		},
		{
			name:      "non-200 is indeterminate",
			responses: []scriptedResponse{{http.StatusServiceUnavailable, ""}},
			want:      false,
			wantCalls: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := &scriptedTransport{responses: tc.responses}
			w := clipVerifyWorker(t, rt)
			got := w.clipConfirmedAbsent(context.Background(), "123", "AbCdEf", 0, 0)
			if got != tc.want {
				t.Errorf("clipConfirmedAbsent = %v, want %v", got, tc.want)
			}
			if rt.callCount() != tc.wantCalls {
				t.Errorf("polls = %d, want %d", rt.callCount(), tc.wantCalls)
			}
		})
	}
}

func TestClipConfirmedAbsentCanceledContextStaysSilent(t *testing.T) {
	rt := &scriptedTransport{}
	w := clipVerifyWorker(t, rt)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Non-zero waits so the done context, not the expired timer, wins the select.
	if w.clipConfirmedAbsent(ctx, "123", "AbCdEf", clipVerifyDelay, clipVerifyRecheck) {
		t.Error("clipConfirmedAbsent = true on canceled context")
	}
	if rt.callCount() != 0 {
		t.Errorf("polls = %d, want 0", rt.callCount())
	}
}

func TestClipFailedText(t *testing.T) {
	if got := clipFailedText("viewer"); !strings.HasPrefix(got, "@viewer ") {
		t.Errorf("clipFailedText with clipper = %q, want @viewer mention", got)
	}
	if got := clipFailedText(""); !strings.HasPrefix(got, "Heads up: ") {
		t.Errorf("clipFailedText without clipper = %q, want generic line", got)
	}
}
