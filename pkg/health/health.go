// Package health serves the tiny HTTP endpoint Kubernetes probes hit.
package health

import (
	"fmt"
	"net/http"
	"time"
)

// Serve starts probe endpoints on addr in a background goroutine.
//
// /healthz is process liveness only: if this handler answers, the container is
// alive and should not be restarted just because a dependency is reconnecting.
//
// /readyz is traffic readiness. ready reports whether the service can do work
// right now (typically the NATS connection status); /readyz returns 503 until
// it does. A nil ready always reports ok.
//
// The returned error channel yields at most one listener error.
func Serve(addr string, ready func() bool) <-chan error {

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if ready != nil && !ready() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	})

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	errs := make(chan error, 1)
	go func() {
		errs <- srv.ListenAndServe()
	}()

	return errs
}
