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
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/env"
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

// ErrDegraded marks a probe failure that degrades the service instead of
// downing it, for checks whose severity is only known at probe time. A static
// dependency declares itself optional once, at wiring time, with Degrades. A
// check that folds in another service's report cannot: that downstream is
// either degraded or down, and only its answer says which. Wrap with %w so the
// downstream's own failing check names survive into the error string.
var ErrDegraded = errors.New("degraded")

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

// Pinger is the slice of *nats.Conn the NATS check needs, so this package
// never imports nats.go. Both methods are satisfied by *nats.Conn directly.
type Pinger interface {
	IsConnected() bool
	FlushTimeout(time.Duration) error
}

// NATS verifies a NATS connection with a real heartbeat round trip instead of
// only reading its cached state.
//
// IsConnected alone is a local flag: it flips false only when the client's own
// machinery has noticed the loss, and a half-open partition (NAT drop, node
// pause, pulled cable) leaves it true indefinitely while every publish drains
// into the void. FlushTimeout sends a PING and blocks on the server's PONG, so
// a probe either heard from a live server or failed within its remaining
// budget — the same heartbeat the client library itself runs, on demand. The
// flag is still checked first so a client that already knows it is down fails
// without spending a round trip.
//
// A nil conn always passes (see Bool): a service that wired no connection has
// no NATS dependency to report on.
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

// CheckResult is one check's outcome inside a Report.
type CheckResult struct {
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	Optional  bool   `json:"optional,omitempty"`
	Error     string `json:"error,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
}

// Report is the /status response body. Status is StatusOK, StatusDegraded or
// StatusDown, and the HTTP status code carries the same verdict so a monitor
// never has to read the body: 200 ok, 207 degraded, 503 down.
//
// 207 (Multi-Status) is the honest code for a mixed answer and, being 2xx, it
// still reads green to any plain "expect 2xx" check. Better Stack then splits
// the two verdicts on expected-status-code lists alone: an availability monitor
// expecting "200,207" pages only on a real outage, while a second monitor
// expecting "200" notifies on the impairment without paging. The earlier design
// left degraded on 200 and put the word in the body, which needed a keyword
// monitor; status codes cost nothing on the free plan and cannot drift out of
// sync with the aggregate the way a body string can.
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

// Add appends checks to a Set during wiring, before it is served.
//
// It exists for the one check that cannot be passed to NewSet: the health RPC
// responder answers out of the Set, so registering it needs the Set to already
// exist, and the check that watches that registration can only be built
// afterwards. Everything else belongs in NewSet, where the Set is complete by
// construction. Nothing serializes this against run, because a Set is finished
// being built before any handler derived from it is mounted.
func (s *Set) Add(checks ...Check) {
	s.checks = append(s.checks, checks...)
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
		r.Optional = r.Optional || errors.Is(err, ErrDegraded)
	}
	return r
}

// Snapshot runs every check and returns the aggregate, for callers answering on
// a transport other than HTTP. The NATS health RPC replies with this, so the
// report a sibling service folds into its own /status is the one this service
// would have served over HTTP at that instant.
func (s *Set) Snapshot(ctx context.Context) Report { return s.run(ctx) }

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
		switch report.Status {
		case StatusDown:
			w.WriteHeader(http.StatusServiceUnavailable)
		case StatusDegraded:
			w.WriteHeader(http.StatusMultiStatus)
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

// tlsEnvConfig reads the TLS_CERT_FILE/TLS_KEY_FILE pair and builds a config
// that re-reads it from disk on every handshake. The files are a
// cert-manager-managed Secret mount: kubelet swaps them in place on renewal,
// so loading per handshake is what makes rotation need no restarts and no
// manual step. Handshake volume here is probes plus a reused traefik
// connection — pennies. The pair is also loaded once up front so an unreadable
// cert fails the boot loudly instead of on the first probe.
//
// Both unset returns a nil config (plaintext listener); a half-set pair is an
// error, never a plaintext fallback.
func tlsEnvConfig() (*tls.Config, error) {
	certFile, keyFile := env.Get("TLS_CERT_FILE", ""), env.Get("TLS_KEY_FILE", "")
	if (certFile == "") != (keyFile == "") {
		return nil, errors.New("health: TLS_CERT_FILE and TLS_KEY_FILE must both be set or both empty")
	}
	if certFile == "" {
		return nil, nil
	}

	load := func() (*tls.Certificate, error) {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("health: load tls key pair: %w", err)
		}
		return &cert, nil
	}
	if _, err := load(); err != nil {
		return nil, err
	}
	return &tls.Config{
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return load() },
	}, nil
}

// Serve starts the health endpoints on addr in a background goroutine. The
// returned error channel yields at most one listener error.
//
// When the TLS_CERT_FILE/TLS_KEY_FILE pair is set (the cert-manager fleet-CA
// cert, mirroring the transactions listener) it serves HTTPS: the fleet's
// traefik->backend hops are TLS with no exceptions, and /status is routed
// through traefik. A half-set pair is an error, never a plaintext fallback;
// the dead listener then fails the pod's probes, which is what makes the
// misconfiguration visible.
func Serve(addr, service string, checks ...Check) <-chan error {
	return ServeSet(addr, NewSet(service, checks...))
}

// ServeSet is Serve over a Set the caller already built. A service that also
// answers the NATS health RPC has to hand the same Set to both surfaces:
// building a second one would let /status and the RPC reply disagree about the
// same service at the same instant.
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
