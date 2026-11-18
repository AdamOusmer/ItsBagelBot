// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package monitor

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

const (
	TraceAPIEndpoint  = "https://trace-api.newrelic.com/trace/v1"
	spanFlushInterval = 5 * time.Second
	spanBatchSize     = 200
	spanBufferSize    = 4096
)

type Span struct {
	TraceID    string
	ID         string
	ParentID   string
	Name       string
	Service    string
	Start      time.Time
	Duration   time.Duration
	Attributes map[string]any
}

type SpanExporter struct {
	endpoint string
	key      string
	client   *http.Client
	log      *zap.Logger
	spans    chan Span
	dropped  atomic.Uint64
}

var processExporter atomic.Pointer[SpanExporter]

func NewSpanExporter(endpoint, key string, log *zap.Logger) *SpanExporter {
	return &SpanExporter{
		endpoint: endpoint,
		key:      key,
		client:   HTTPClient(10 * time.Second),
		log:      log,
		spans:    make(chan Span, spanBufferSize),
	}
}

func InstallExporter(exporter *SpanExporter) {
	processExporter.Store(exporter)
}

func ExportSpan(span Span) {
	if exporter := processExporter.Load(); exporter != nil {
		exporter.Export(span)
	}
}

func SpanID() string {
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	return hex.EncodeToString(id)
}

func (e *SpanExporter) Export(span Span) {
	select {
	case e.spans <- span:
	default:
		e.dropped.Add(1)
	}
}

func (e *SpanExporter) Run(ctx context.Context) {
	ticker := time.NewTicker(spanFlushInterval)
	defer ticker.Stop()
	batch := make([]Span, 0, spanBatchSize)
	for {
		select {
		case <-ctx.Done():
			e.flush(e.drain(batch))
			return
		case span := <-e.spans:
			batch = append(batch, span)
			if len(batch) >= spanBatchSize {
				batch = e.flush(batch)
			}
		case <-ticker.C:
			batch = e.flush(batch)
		}
	}
}

func (e *SpanExporter) drain(batch []Span) []Span {
	for {
		select {
		case span := <-e.spans:
			batch = append(batch, span)
		default:
			return batch
		}
	}
}

func (e *SpanExporter) flush(batch []Span) []Span {
	if len(batch) == 0 {
		return batch
	}
	if err := e.post(batch); err != nil {
		e.log.Warn("trace api export failed", zap.Int("spans", len(batch)),
			zap.Uint64("dropped", e.dropped.Load()), zap.Error(err))
	}
	return batch[:0]
}

type traceAPIBatch struct {
	Spans []traceAPISpan `json:"spans"`
}

type traceAPISpan struct {
	TraceID    string         `json:"trace.id"`
	ID         string         `json:"id"`
	Timestamp  int64          `json:"timestamp"`
	Attributes map[string]any `json:"attributes"`
}

func wireSpan(span Span) traceAPISpan {
	attributes := make(map[string]any, len(span.Attributes)+4)
	for key, value := range span.Attributes {
		attributes[key] = value
	}
	attributes["name"] = span.Name
	attributes["service.name"] = span.Service
	attributes["duration.ms"] = float64(span.Duration.Microseconds()) / 1000
	if span.ParentID != "" {
		attributes["parent.id"] = span.ParentID
	}
	return traceAPISpan{TraceID: span.TraceID, ID: span.ID, Timestamp: span.Start.UnixMilli(), Attributes: attributes}
}

func (e *SpanExporter) post(batch []Span) error {
	spans := make([]traceAPISpan, len(batch))
	for i := range batch {
		spans[i] = wireSpan(batch[i])
	}
	body, err := json.Marshal([]traceAPIBatch{{Spans: spans}})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Api-Key", e.key)
	req.Header.Set("Data-Format", "newrelic")
	req.Header.Set("Data-Format-Version", "1")
	res, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		return &traceAPIStatusError{status: res.StatusCode}
	}
	return nil
}

type traceAPIStatusError struct{ status int }

func (e *traceAPIStatusError) Error() string {
	return "trace api answered " + http.StatusText(e.status)
}
