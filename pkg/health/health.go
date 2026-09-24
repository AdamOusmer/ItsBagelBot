// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package health

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/tlsenv"
)

const (
	checkTimeout = 5 * time.Second
	drainDelay   = 10 * time.Second
)

const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
	StatusDown     = "down"
)

var ErrDegraded = errors.New("degraded")

type Check struct {
	Name     string
	Probe    func(ctx context.Context) error
	Optional bool
}

func Bool(name string, ok func() bool) Check {
	return Check{Name: name, Probe: func(context.Context) error {
		if ok != nil && !ok() {
			return errors.New("not ok")
		}
		return nil
	}}
}

func Degrades(c Check) Check {
	c.Optional = true
	return c
}

type Pinger interface {
	IsConnected() bool
	FlushTimeout(time.Duration) error
}

func NATS(name string, conn Pinger) Check {
	return Check{Name: name, Probe: func(ctx context.Context) error {
		if conn == nil {
			return nil
		}
		if !conn.IsConnected() {
			return errors.New("not connected")
		}
		wait := checkTimeout
		if deadline, ok := ctx.Deadline(); ok {
			if d := time.Until(deadline); d < wait {
				wait = d
			}
		}
		if wait <= 0 {
			return errors.New("heartbeat: probe deadline exceeded")
		}
		if err := conn.FlushTimeout(wait); err != nil {
			return fmt.Errorf("heartbeat: %w", err)
		}
		return nil
	}}
}

type CheckResult struct {
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	Optional  bool   `json:"optional,omitempty"`
	Error     string `json:"error,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
}

type Report struct {
	Service string        `json:"service"`
	Status  string        `json:"status"`
	Checks  []CheckResult `json:"checks"`
}

type Set struct {
	service string
	checks  []Check
	live    []Check
}

func NewSet(service string, checks ...Check) *Set {
	return &Set{service: service, checks: checks}
}

func (s *Set) Add(checks ...Check) {
	s.checks = append(s.checks, checks...)
}

// Only for process-local state: a dependency here turns a partial outage into rolling restarts.
func (s *Set) Live(checks ...Check) {
	s.live = append(s.live, checks...)
}

func (s *Set) runLive(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	for _, c := range s.live {
		if c.Probe == nil {
			continue
		}
		if err := c.Probe(ctx); err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
	}
	return nil
}

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
		r.Optional = r.Optional || errors.Is(err, ErrDegraded)
	}
	return r
}

func (s *Set) Snapshot(ctx context.Context) Report { return s.run(ctx) }

func (s *Set) Liveness() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.runLive(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	}
}

func (s *Set) Readiness() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.run(r.Context()).Status == StatusDown {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	}
}

func (s *Set) Status() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report := s.run(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		switch report.Status {
		case StatusDown:
			w.WriteHeader(http.StatusServiceUnavailable)
		case StatusDegraded:
			w.WriteHeader(http.StatusMultiStatus)
		}
		body, err := codec.Marshal(report)
		if err != nil {
			body = []byte(`{"status":"` + report.Status + `"}`)
		}
		_, _ = w.Write(body)
	}
}

func Drain() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(drainDelay)
		fmt.Fprintln(w, "ok")
	}
}

func (s *Set) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.Liveness())
	mux.HandleFunc("/readyz", s.Readiness())
	mux.HandleFunc("/status", s.Status())
	mux.HandleFunc("/drain", Drain())
	return mux
}

func tlsEnvConfig() (*tls.Config, error) {
	pair, err := tlsenv.PairFromEnv("TLS_CERT_FILE", "TLS_KEY_FILE")
	if err != nil {
		return nil, err
	}
	return pair.ServerConfig()
}

func Serve(addr, service string, checks ...Check) <-chan error {
	return ServeSet(addr, NewSet(service, checks...))
}

func ServeSet(addr string, set *Set) <-chan error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           set.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errs := make(chan error, 1)
	cfg, err := tlsEnvConfig()
	if err != nil {
		errs <- err
		return errs
	}
	srv.TLSConfig = cfg

	go func() {
		if srv.TLSConfig != nil {
			errs <- srv.ListenAndServeTLS("", "")
			return
		}
		errs <- srv.ListenAndServe()
	}()

	return errs
}
