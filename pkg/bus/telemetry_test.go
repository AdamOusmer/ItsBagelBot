package bus

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
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

// collectorRecorder answers like the fake collector and keeps every payload the
// agent posted, so a test can assert on what was actually harvested rather than
// on the agent's internal buffers, which are not reachable from outside the
// go-agent module.
type collectorRecorder struct {
	mu    sync.Mutex
	posts []string
}

func (r *collectorRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		if payload, err := readCollectorPayload(req); err == nil {
			r.mu.Lock()
			r.posts = append(r.posts, req.URL.Query().Get("method")+" "+payload)
			r.mu.Unlock()
		}
	}
	return localCollectorResponse(req)
}

// posted reports whether any harvested payload contains needle.
func (r *collectorRecorder) posted(needle string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, post := range r.posts {
		if strings.Contains(post, needle) {
			return true
		}
	}
	return false
}

// readCollectorPayload undoes the agent's transport encoding. Small payloads go
// out as plain JSON and larger ones gzipped, so both have to be understood or
// the assertion would silently depend on payload size.
func readCollectorPayload(req *http.Request) (string, error) {
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return "", err
	}
	if req.Header.Get("Content-Encoding") != "gzip" {
		return string(raw), nil
	}
	reader, err := gzip.NewReader(strings.NewReader(string(raw)))
	if err != nil {
		return "", err
	}
	defer reader.Close()
	plain, err := io.ReadAll(reader)
	return string(plain), err
}

// newLocalApplication builds a fully connected agent that never leaves the
// process, so tests can assert on real transactions (a nil application makes
// StartTransaction return nil and hides every difference between the sampled and
// unsampled paths). transport may be nil for the plain fake collector.
func newLocalApplication(t *testing.T, transport http.RoundTripper) *newrelic.Application {
	t.Helper()

	if transport == nil {
		transport = roundTripFunc(localCollectorResponse)
	}

	app, err := newrelic.NewApplication(func(cfg *newrelic.Config) {
		cfg.AppName = "bus-telemetry-test"
		cfg.License = strings.Repeat("a", 40)
		cfg.Enabled = true
		cfg.DistributedTracer.Enabled = true
		cfg.Transport = transport
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.Shutdown(time.Second) })

	if err := app.WaitForConnection(time.Second); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestRPCTraceHeadersRoundTripWithoutCollector(t *testing.T) {
	app := newLocalApplication(t, nil)

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

func TestParseSampleRateFailsTowardSamplingEverything(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want uint64
	}{
		{"", 1},
		{"0", 1},
		{"-1", 1},
		{"every", 1},
		{"1", 1},
		{"100", 100},
	} {
		if got := parseSampleRate(tc.raw); got != tc.want {
			t.Fatalf("parseSampleRate(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

func TestLaneSamplerPicksOneInNStartingAtTheFirst(t *testing.T) {
	for _, tc := range []struct {
		rate uint64
		want []bool
	}{
		{1, []bool{true, true, true, true}},
		{3, []bool{true, false, false, true, false, false, true}},
	} {
		stats := &laneStats{sampleRate: tc.rate}
		for i, want := range tc.want {
			if got := stats.sample(); got != want {
				t.Fatalf("rate %d: sample %d = %v, want %v", tc.rate, i+1, got, want)
			}
		}
	}
}

func TestLaneSamplerIsRateExactUnderConcurrency(t *testing.T) {
	const rate, messages, routines = 10, 1000, 8

	stats := &laneStats{sampleRate: rate}
	var sampled atomic.Uint64
	var wg sync.WaitGroup

	for r := 0; r < routines; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < messages/routines; i++ {
				if stats.sample() {
					sampled.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	if got := sampled.Load(); got != messages/rate {
		t.Fatalf("sampled %d of %d, want exactly %d", got, messages, messages/rate)
	}
}

func TestLaneStatsDrainAggregatesAndResets(t *testing.T) {
	stats := &laneStats{destination: "twitch.ingress.event.premium", sampleRate: 100}
	stats.record(resultOK, 2*time.Millisecond)
	stats.record(resultOK, 4*time.Millisecond)
	stats.record(resultDeferred, 0)
	stats.record("error", time.Millisecond)

	params, ok := stats.drain()
	if !ok {
		t.Fatal("drain reported an empty window after four deliveries")
	}
	for key, want := range map[string]float64{
		"processed": 4, "ok": 2, "deferred": 1, "failed": 1,
		"queue_ms.avg": 1.75, "queue_ms.max": 4, "sample_rate": 100,
	} {
		if got := params[key]; got != want {
			t.Fatalf("drain %s = %v, want %v", key, got, want)
		}
	}
	if got := params[messagingDestinationAttribute]; got != "twitch.ingress.event.premium" {
		t.Fatalf("drain destination = %v", got)
	}

	// The window is consumed by the read: a silent lane must emit nothing rather
	// than a zero event per interval.
	if _, ok := stats.drain(); ok {
		t.Fatal("second drain reported a window, want the counters reset and silent")
	}
}

func TestLaneTelemetryInternsCountersByDestination(t *testing.T) {
	registry := newLaneTelemetry()

	premium := registry.register(nil, "twitch.ingress.event.premium", 7)
	// A second consumer unit on the same lane must share the cursor and counters,
	// or the sample rate would multiply by the unit count.
	again := registry.register(nil, "twitch.ingress.event.premium", 7)
	if premium != again {
		t.Fatal("re-registering a lane created a second counter set")
	}
	// Subjects that share a normalized family share one entry: that is what keeps
	// the registry bounded when subjects carry identifiers.
	if sibling := registry.register(nil, "bagel.rpc.users.get", 7); sibling == premium {
		t.Fatal("different destinations shared one counter set")
	}
	if standard := registry.register(nil, "twitch.ingress.event.standard", 7); standard == premium {
		t.Fatal("distinct ingress lanes shared one counter set")
	}
	if got := len(registry.lanes); got != 3 {
		t.Fatalf("registry holds %d lanes, want 3", got)
	}
	if premium.sampleRate != 7 {
		t.Fatalf("lane sample rate = %d, want 7", premium.sampleRate)
	}
}

func TestLaneTelemetryFlushIsSafeWithoutAnApplication(t *testing.T) {
	registry := newLaneTelemetry()
	stats := registry.register(nil, "bagel.rpc.commands.run", 1)
	stats.record(resultOK, time.Millisecond)

	// A nil application starts no flusher, and flushing by hand must still be a
	// silent no-op rather than a panic — this is the local-development shape.
	registry.flush(nil)

	if _, ok := stats.drain(); ok {
		t.Fatal("flush left the window unread")
	}
}

func TestLaneTelemetryFlushEmitsCustomEvents(t *testing.T) {
	recorder := &collectorRecorder{}
	app := newLocalApplication(t, recorder)

	registry := newLaneTelemetry()
	stats := registry.register(app, "twitch.ingress.event.premium", 100)
	stats.record(resultOK, 3*time.Millisecond)
	stats.record("error", time.Millisecond)
	registry.flush(app)

	// Shutdown forces the final harvest, so the assertion is on what the agent
	// actually posted rather than on an internal buffer.
	app.Shutdown(2 * time.Second)

	if !recorder.posted(laneTelemetryEventType) {
		t.Fatalf("no %s event was harvested; posts=%v", laneTelemetryEventType, recorder.posts)
	}
	if !recorder.posted("twitch.ingress.event.premium") {
		t.Fatal("harvested event carried no lane destination")
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
