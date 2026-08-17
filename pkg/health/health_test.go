// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"ItsBagelBot/pkg/codec"
)

func failing(name string) Check {
	return Check{Name: name, Probe: func(context.Context) error { return errors.New("dial timeout") }}
}

func passing(name string) Check {
	return Bool(name, func() bool { return true })
}

func get(t *testing.T, s *Set, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) Report {
	t.Helper()
	var report Report
	if err := codec.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return report
}

// One table drives every endpoint-verdict combination: which checks fail and
// whether they are optional decides the HTTP code per endpoint and the
// aggregate the /status body reports.
func TestEndpointVerdicts(t *testing.T) {
	cases := []struct {
		name       string
		set        *Set
		path       string
		wantCode   int
		wantStatus string // /status only; empty skips the body assertion
	}{
		{"liveness ignores failing checks", NewSet("svc", failing("nats")), "/healthz", http.StatusOK, ""},
		{"ready with no checks", NewSet("svc"), "/readyz", http.StatusOK, ""},
		{"ready when all pass", NewSet("svc", passing("nats")), "/readyz", http.StatusOK, ""},
		{"critical failure is not ready", NewSet("svc", failing("nats")), "/readyz", http.StatusServiceUnavailable, ""},
		{"optional failure stays ready", NewSet("svc", passing("nats"), Degrades(failing("valkey"))), "/readyz", http.StatusOK, ""},
		{"status ok", NewSet("svc", passing("nats")), "/status", http.StatusOK, StatusOK},
		{"status degraded is still up", NewSet("svc", passing("nats"), Degrades(failing("valkey"))), "/status", http.StatusOK, StatusDegraded},
		{"status down", NewSet("svc", failing("nats"), Degrades(failing("valkey"))), "/status", http.StatusServiceUnavailable, StatusDown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := get(t, tc.set, tc.path)
			if rec.Code != tc.wantCode {
				t.Fatalf("%s = %d, want %d", tc.path, rec.Code, tc.wantCode)
			}
			if tc.wantStatus != "" && decode(t, rec).Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", decode(t, rec).Status, tc.wantStatus)
			}
		})
	}
}

func TestStatusReportShape(t *testing.T) {
	rec := get(t, NewSet("svc", passing("nats"), Degrades(failing("valkey"))), "/status")
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}

	report := decode(t, rec)
	for i := range report.Checks {
		report.Checks[i].LatencyMS = 0 // wall-clock, not asserted
	}
	want := Report{Service: "svc", Status: StatusDegraded, Checks: []CheckResult{
		{Name: "nats", OK: true},
		{Name: "valkey", Optional: true, Error: "dial timeout"},
	}}
	if !reflect.DeepEqual(report, want) {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
}

func TestBoolNilAlwaysPasses(t *testing.T) {
	if err := Bool("x", nil).Probe(context.Background()); err != nil {
		t.Fatalf("nil ok func: %v", err)
	}
}

func TestProbeSeesDeadline(t *testing.T) {
	s := NewSet("svc", Check{Name: "slow", Probe: func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			return errors.New("no deadline")
		}
		return nil
	}})
	if rec := get(t, s, "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("readyz = %d, want 200 (probe must see a deadline)", rec.Code)
	}
}
