package bus

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func localCollectorResponse(req *http.Request) (*http.Response, error) {
	body := `{"return_value":[]}`
	switch req.URL.Query().Get("method") {
	case "preconnect":
		body = `{"return_value":{"redirect_host":"collector.invalid"}}`
	case "connect":
		body = `{"return_value":{"agent_run_id":"local","account_id":"123","trusted_account_key":"123","primary_application_id":"456"}}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func TestRPCTraceHeadersRoundTripWithoutCollector(t *testing.T) {
	app, err := newrelic.NewApplication(func(cfg *newrelic.Config) {
		cfg.AppName = "bus-telemetry-test"
		cfg.License = strings.Repeat("a", 40)
		cfg.Enabled = true
		cfg.DistributedTracer.Enabled = true
		cfg.Transport = roundTripFunc(localCollectorResponse)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown(time.Second)
	if err := app.WaitForConnection(time.Second); err != nil {
		t.Fatal(err)
	}

	parent := app.StartTransaction("parent")
	msg := nats.NewMsg("bagel.rpc.test")
	insertTraceHeaders(newrelic.NewContext(context.Background(), parent), msg)

	traceparent := msg.Header.Get(newrelic.DistributedTraceW3CTraceParentHeader)
	if traceparent == "" {
		t.Fatalf("request message has no W3C traceparent header (metadata=%+v, headers=%v)", parent.GetTraceMetadata(), msg.Header)
	}
	parts := strings.Split(traceparent, "-")
	if len(parts) != 4 {
		t.Fatalf("invalid traceparent %q", traceparent)
	}

	child := app.StartTransaction("child")
	acceptTraceHeaders(child, msg.Header)
	if got := child.GetTraceMetadata().TraceID; got != parts[1] {
		t.Fatalf("accepted trace id = %q, want %q", got, parts[1])
	}
}

func TestMessageDeliveryWaitIsBoundedAtZero(t *testing.T) {
	msg := NewMessage("id", nil)
	msg.receivedAt = time.Now().Add(-2 * time.Millisecond)
	if got := msg.deliveryWait(time.Now()); got < time.Millisecond {
		t.Fatalf("delivery wait = %v, want at least 1ms", got)
	}
	msg.receivedAt = time.Now().Add(time.Second)
	if got := msg.deliveryWait(time.Now()); got != 0 {
		t.Fatalf("future delivery wait = %v, want 0", got)
	}
}

func TestMessagingResultsAreFinite(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "ok"},
		{context.DeadlineExceeded, "timeout"},
		{nats.ErrTimeout, "timeout"},
		{context.Canceled, "error"},
	} {
		if got := messagingResult(tc.err); got != tc.want {
			t.Fatalf("messagingResult(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestDestinationFamiliesAreBounded(t *testing.T) {
	for _, tc := range []struct {
		subject string
		want    string
	}{
		{"bagel.rpc.projector.dashboard.commands.get", "bagel.rpc.projector"},
		{"bagel.rpc.projector.tenant-123.private", "bagel.rpc.projector"},
		{"bagel.rpc.untrusted.tenant-123", "bagel.rpc.other"},
		{"twitch.ingress.event.premium", "twitch.ingress.event.premium"},
		{"twitch.ingress.event.tenant-123", "twitch.ingress.event.other"},
		{"arbitrary.tenant-123", "other"},
	} {
		if got := normalizedDestination(tc.subject); got != tc.want {
			t.Fatalf("normalizedDestination(%q) = %q, want %q", tc.subject, got, tc.want)
		}
	}
}
