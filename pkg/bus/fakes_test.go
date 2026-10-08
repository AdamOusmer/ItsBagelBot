// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

func newTestBatchPublisher() *batchPublisher {
	publisher := &batchPublisher{log: zap.NewNop()}
	publisher.signal = sync.NewCond(&publisher.stateMu)
	return publisher
}

func stagedBatch(n int) []publishRequest {
	batch := make([]publishRequest, 0, n)
	for i := range n {
		msg := nats.NewMsg("data.test.batch")
		msg.Header.Set(messageIDHeader, "id-"+strconv.Itoa(i+1))
		batch = append(batch, publishRequest{msg: msg})
	}
	return batch
}

type pullConsumerHandle struct {
	jsapi.Consumer
	info *jsapi.ConsumerInfo
}

func (c *pullConsumerHandle) Info(context.Context) (*jsapi.ConsumerInfo, error) {
	return c.info, nil
}

type fakePullMsg struct {
	sequence uint64
	header   nats.Header
	payload  []byte

	mu     sync.Mutex
	acked  int
	nakked int
}

func (m *fakePullMsg) acks() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.acked
}

func (m *fakePullMsg) Metadata() (*jsapi.MsgMetadata, error) {
	return &jsapi.MsgMetadata{
		Domain:   "hub",
		Stream:   TwitchIngressStream.Name,
		Consumer: "worker_twitch_ingress_event_standard",
		Sequence: jsapi.SequencePair{Stream: m.sequence, Consumer: m.sequence},
	}, nil
}

func (m *fakePullMsg) Data() []byte         { return m.payload }
func (m *fakePullMsg) Headers() nats.Header { return m.header }
func (m *fakePullMsg) Subject() string      { return "twitch.ingress.event.standard" }
func (m *fakePullMsg) Reply() string        { return "$JS.ACK.hub.x.TWITCH_INGRESS.c.1.1.1.0.0" }
func (m *fakePullMsg) DoubleAck(context.Context) error {
	panic("pull lane must never double-ack")
}
func (m *fakePullMsg) NakWithDelay(time.Duration) error { return m.Nak() }
func (m *fakePullMsg) InProgress() error                { return nil }
func (m *fakePullMsg) Term() error                      { return nil }
func (m *fakePullMsg) TermWithReason(string) error      { return nil }

func (m *fakePullMsg) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked++
	return nil
}

func (m *fakePullMsg) Nak() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nakked++
	return nil
}

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

func newLocalApplication(t *testing.T) *newrelic.Application {
	t.Helper()
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
	t.Cleanup(func() { app.Shutdown(time.Second) })

	if err := app.WaitForConnection(time.Second); err != nil {
		t.Fatal(err)
	}
	return app
}
