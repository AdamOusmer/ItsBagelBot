// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"ItsBagelBot/pkg/codec"
)

func failing(name string) Check {
	return Check{Name: name, Probe: func(context.Context) error { return errors.New("dial timeout") }}
}

func passing(name string) Check {
	return Bool(name, func() bool { return true })
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
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

func TestLivenessAlwaysOK(t *testing.T) {
	s := NewSet("svc", failing("nats"))
	if rec := get(t, s.Handler(), "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d, want 200", rec.Code)
	}
}

func TestReadinessGatesOnCriticalChecks(t *testing.T) {
	up := true
	s := NewSet("svc", Bool("nats", func() bool { return up }))

	if rec := get(t, s.Handler(), "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("readyz up = %d, want 200", rec.Code)
	}
	up = false
	if rec := get(t, s.Handler(), "/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz down = %d, want 503", rec.Code)
	}
}

func TestReadinessNoChecksOK(t *testing.T) {
	if rec := get(t, NewSet("svc").Handler(), "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("readyz = %d, want 200", rec.Code)
	}
}

func TestOptionalFailureDegradesButStaysReady(t *testing.T) {
	s := NewSet("svc", passing("nats"), Degrades(failing("valkey")))

	if rec := get(t, s.Handler(), "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("readyz = %d, want 200 (optional failure must not evict the pod)", rec.Code)
	}

	rec := get(t, s.Handler(), "/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (degraded is still up)", rec.Code)
	}
	report := decode(t, rec)
	if report.Status != StatusDegraded {
		t.Fatalf("status = %q, want %q", report.Status, StatusDegraded)
	}
}

func TestCriticalFailureIsDown(t *testing.T) {
	s := NewSet("svc", failing("nats"), Degrades(failing("valkey")), passing("mysql"))

	rec := get(t, s.Handler(), "/status")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if report := decode(t, rec); report.Status != StatusDown {
		t.Fatalf("status = %q, want %q", report.Status, StatusDown)
	}
}

func TestStatusReportsPerCheck(t *testing.T) {
	s := NewSet("svc", passing("nats"), failing("mysql"))

	rec := get(t, s.Handler(), "/status")
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	report := decode(t, rec)
	if report.Service != "svc" || len(report.Checks) != 2 {
		t.Fatalf("report = %+v", report)
	}

	byName := map[string]CheckResult{}
	for _, c := range report.Checks {
		byName[c.Name] = c
	}
	if !byName["nats"].OK || byName["nats"].Error != "" {
		t.Fatalf("nats = %+v", byName["nats"])
	}
	if byName["mysql"].OK || byName["mysql"].Error != "dial timeout" {
		t.Fatalf("mysql = %+v", byName["mysql"])
	}
}

func TestStatusAllHealthy(t *testing.T) {
	s := NewSet("svc", passing("nats"))

	rec := get(t, s.Handler(), "/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if report := decode(t, rec); report.Status != StatusOK {
		t.Fatalf("status = %q, want %q", report.Status, StatusOK)
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
	if rec := get(t, s.Handler(), "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("readyz = %d, want 200 (probe must see a deadline)", rec.Code)
	}
}
