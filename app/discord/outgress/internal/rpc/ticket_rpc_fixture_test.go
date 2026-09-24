// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"

	"go.uber.org/zap"
)

type recordedCall struct {
	method      string
	path        string
	query       string
	body        string
	contentType string
}

type scriptedTransport struct {
	mu    sync.Mutex
	calls []recordedCall
	reply func(call recordedCall) (int, string)
}

func (s *scriptedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	call := recordedCall{
		method: r.Method, path: strings.TrimPrefix(r.URL.Path, "/api/v10"),
		query: r.URL.RawQuery, contentType: r.Header.Get("Content-Type"),
	}
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		call.body = string(raw)
	}
	s.mu.Lock()
	s.calls = append(s.calls, call)
	s.mu.Unlock()

	status, body := 200, `{"id":"m-new"}`
	if s.reply != nil {
		status, body = s.reply(call)
	}
	rec := httptest.NewRecorder()
	rec.Code = status
	rec.Body.WriteString(body)
	return rec.Result(), nil
}

func (s *scriptedTransport) find(method, path string) []recordedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []recordedCall
	for _, c := range s.calls {
		if c.method == method && c.path == path {
			out = append(out, c)
		}
	}
	return out
}

func newTicketRPC(t *testing.T, reply func(recordedCall) (int, string)) (*ticketRPC, *scriptedTransport) {
	t.Helper()
	tr := &scriptedTransport{reply: reply}
	client := discapi.NewClient("bot-token")
	client.SetTransport(tr)
	return &ticketRPC{rest: client, botID: "bot9", log: zap.NewNop()}, tr
}

type onceMemo struct{ claimed map[int]bool }

func newOnceMemo() *onceMemo { return &onceMemo{claimed: map[int]bool{}} }

func (m *onceMemo) ClaimSummary(_ context.Context, ticketID int) bool {
	if ticketID <= 0 {
		return true
	}
	if m.claimed[ticketID] {
		return false
	}
	m.claimed[ticketID] = true
	return true
}

func (s *scriptedTransport) indexOf(method, path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.calls {
		if c.method == method && c.path == path {
			return i
		}
	}
	return -1
}

func messagePage(high, n int) string {
	items := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := high - i
		items = append(items, fmt.Sprintf(
			`{"id":"%d","content":"line %d","timestamp":"2026-01-02T03:04:05+00:00","author":{"id":"u1","username":"ada"}}`,
			id, id))
	}
	return "[" + strings.Join(items, ",") + "]"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
