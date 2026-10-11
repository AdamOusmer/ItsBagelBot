// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
)

func startPprofServer(addr, mode string) {
	if addr == "" || !profiledMode(mode) {
		return
	}
	runtime.SetMutexProfileFraction(5)
	runtime.SetBlockProfileRate(100000)
	go func() {
		if err := http.ListenAndServe(addr, pprofMux()); err != nil {
			fmt.Fprintln(os.Stderr, "bus-bench: pprof-addr:", err)
		}
	}()
}

func pprofMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}
