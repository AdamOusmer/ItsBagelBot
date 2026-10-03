// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"net/http"
	"testing"

	"ItsBagelBot/pkg/bus"
)

type cannedTransport struct{ status int }

func (t cannedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: t.status, Body: http.NoBody}, nil
}

func benchWorker(tb testing.TB) *Worker {
	tb.Helper()
	return pipelineWorker(tb, cannedTransport{status: http.StatusNoContent})
}

const benchChatBody = `{"type":"chat","broadcaster_id":"44322889","payload":{"broadcaster_id":"44322889","message":"hello chat"}}`

func BenchmarkProcessChat(b *testing.B) {
	w := benchWorker(b)
	payload := []byte(benchChatBody)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := w.Process(bus.NewMessage("bench", payload)); err != nil {
			b.Fatal(err)
		}
	}
}

func TestProcessChatAllocCeiling(t *testing.T) {
	w := benchWorker(t)
	payload := []byte(benchChatBody)
	const ceiling = 96.0

	allocs := testing.AllocsPerRun(500, func() {
		if err := w.Process(bus.NewMessage("bench", payload)); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > ceiling {
		t.Fatalf("chat hot path allocates %.1f allocs/op, ceiling %.0f", allocs, ceiling)
	}
}
