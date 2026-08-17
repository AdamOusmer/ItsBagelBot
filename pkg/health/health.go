// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package health is the shared health surface for the fleet's Go services. It
// serves two audiences from one set of named checks: Kubernetes probes
// (/healthz, /readyz, /drain) and external uptime monitors — the Better Stack
// status page polls /status and gets a JSON report of every dependency check
// with per-check latency, so a monitor can alert on a single degraded
// dependency instead of a binary up/down.
//
// A service declares what "healthy" means once, as Checks, and every endpoint
// derives from them: /readyz gates pod traffic on the same checks /status
// reports upstream, so the status page can never disagree with what Kubernetes
// is routing to.
package health

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"ItsBagelBot/pkg/codec"
)

// checkTimeout bounds one endpoint invocation end to end. Probes and uptime
// monitors both treat a slow answer as a failed one, so a hung dependency
// check must turn into a reported failure before the caller gives up on the
// request itself.
const checkTimeout = 5 * time.Second

// Aggregate status values reported by /status. StatusDown means a critical
// dependency is gone and the service cannot do its job — /readyz turns 503 and
// Kubernetes stops routing to the pod. StatusDegraded means only optional
// dependencies are failing: the service keeps serving (200, still ready) but
// the status page should show impairment. StatusOK is everything passing.
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
	StatusDown     = "down"
)

// Check is one named dependency verdict: NATS connected, database reachable.
// Probe returns nil when the dependency is usable and must honor ctx — it runs
// on every /readyz and /status request.
type Check struct {
	Name  string
	Probe func(ctx context.Context) error
	// Optional marks a dependency the service can limp along without. Its
	// failure reports degraded instead of down: /status says so, but /readyz
	// stays ready — evicting every pod because a nice-to-have is unreachable
	// would turn a partial outage into a full one.
	Optional bool
}

// Bool adapts the func() bool shape most clients already expose
// (nats.Conn.IsConnected) into a Check. A nil ok always passes.
func Bool(name string, ok func() bool) Check {
	return Check{Name: name, Probe: func(context.Context) error {
		if ok != nil && !ok() {
			return errors.New("not ok")
		}
		return nil
	}}
}

// Degrades marks a check optional: its failure degrades the service instead
// of downing it. Reads at the call site as what the failure does.
func Degrades(c Check) Check {
	c.Optional = true
	return c
}

// CheckResult is one check's outcome inside a Report.
type CheckResult struct {
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	Optional  bool   `json:"optional,omitempty"`
	Error     string `json:"error,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
}

// Report is the /status response body. Status is StatusOK, StatusDegraded or
// StatusDown; the HTTP status code carries only the down verdict (503), while
// degraded still answers 200 — a degraded service is up. A Better Stack
// availability monitor therefore just expects 2xx, and a second keyword
// monitor alerting on "degraded" in the body drives the status page's
// degraded-performance state without ever paging as a full outage.
type Report struct {
	Service string        `json:"service"`
	Status  string        `json:"status"`
	Checks  []CheckResult `json:"checks"`
}

// Set is a service's named checks plus the handlers derived from them.
type Set struct {
	service string
	checks  []Check
}

func NewSet(service string, checks ...Check) *Set {
	return &Set{service: service, checks: checks}
}

// run executes every check concurrently and reports the aggregate. Concurrent
// because the checks are independent by construction and the caller is a probe
// with its own deadline: total cost is the slowest check, not the sum.
func (s *Set) run(ctx context.Context) Report {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	results := make([]CheckResult, len(s.checks))
	var wg sync.WaitGroup
	for i, c := range s.checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = c.result(ctx)
		}()
	}
	wg.Wait()

	return Report{Service: s.service, Status: aggregate(results), Checks: results}
}

// aggregate folds per-check outcomes into the service verdict: any critical
// failure is down, otherwise any failure is degraded, otherwise ok.
func aggregate(results []CheckResult) string {
	status := StatusOK
	for _, r := range results {
		if r.OK {
			continue
		}
		if !r.Optional {
			return StatusDown
		}
		status = StatusDegraded
	}
	return status
}

func (c Check) result(ctx context.Context) CheckResult {
	start := time.Now()
	err := c.Probe(ctx)
	r := CheckResult{
		Name:      c.Name,
		OK:        err == nil,
		Optional:  c.Optional,
		LatencyMS: time.Since(start).Milliseconds(),
	}
	if err != nil {
		r.Error = err.Error()
	}
	return r
}

// Liveness answers /healthz: process liveness only. If this handler answers,
// the container is alive and should not be restarted just because a dependency
// is reconnecting — that is what /readyz is for.
func (s *Set) Liveness() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	}
}

// Readiness answers /readyz: traffic readiness. 503 only when the aggregate is
// down — a degraded service still takes traffic, so Kubernetes keeps routing
// to it while the status page reports the impairment.
func (s *Set) Readiness() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.run(r.Context()).Status == StatusDown {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	}
}

// Status answers /status for external uptime monitors: the full JSON Report,
// 503 when down and 200 otherwise (degraded included — see Report).
func (s *Set) Status() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report := s.run(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if report.Status == StatusDown {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		body, err := codec.Marshal(report)
		if err != nil {
			// Unreachable with this Report shape; keep the monitor contract
			// (a body always follows the status code) rather than panic.
			body = []byte(`{"status":"` + report.Status + `"}`)
		}
		_, _ = w.Write(body)
	}
}

// Drain answers /drain: used by the Kubernetes preStop hook to delay SIGTERM
// while endpoints drain. Avoids 500 errors during rolling restarts in scratch
// containers without sleep.
func Drain() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Second)
		fmt.Fprintln(w, "ok")
	}
}

// Handler mounts every endpoint on one mux, for services whose only HTTP
// surface is health.
func (s *Set) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.Liveness())
	mux.HandleFunc("/readyz", s.Readiness())
	mux.HandleFunc("/status", s.Status())
	mux.HandleFunc("/drain", Drain())
	return mux
}

// Serve starts the health endpoints on addr in a background goroutine. The
// returned error channel yields at most one listener error.
func Serve(addr, service string, checks ...Check) <-chan error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           NewSet(service, checks...).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		errs <- srv.ListenAndServe()
	}()

	return errs
}
