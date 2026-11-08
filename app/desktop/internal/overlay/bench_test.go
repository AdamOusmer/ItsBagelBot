// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlay

import (
	"net"
	"testing"
	"testing/fstest"

	"github.com/valyala/fasthttp"
)

func benchmarkServer(b *testing.B) *Server {
	b.Helper()
	source, err := OpenSource(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	if err := source.Publish(validScene); err != nil {
		b.Fatal(err)
	}
	server, err := NewServer(source, fstest.MapFS{"index.html": {Data: []byte("renderer")}})
	if err != nil {
		b.Fatal(err)
	}
	return server
}

// Measure the full Fiber routing/security/response path with reused fasthttp
// contexts. Socket acceptance, cold pool misses and test-client encoding are
// deliberately outside this steady-state handler benchmark.
func BenchmarkHandler(b *testing.B) {
	for _, path := range []string{"/state?after=0", "/"} {
		b.Run(path, func(b *testing.B) {
			server := benchmarkServer(b)
			handler := server.app.Handler()
			var request fasthttp.Request
			request.SetRequestURI(sourceOrigin + path)
			request.Header.SetMethod("GET")
			request.Header.Set("Authorization", server.source.current.Load().bearer)
			var ctx fasthttp.RequestCtx
			ctx.Init(&request, &net.TCPAddr{}, nil)
			handler(&ctx)
			if ctx.Response.StatusCode() != 200 {
				b.Fatalf("status %d", ctx.Response.StatusCode())
			}
			b.ReportAllocs()
			for b.Loop() {
				ctx.Response.Reset()
				handler(&ctx)
			}
		})
	}
}

func BenchmarkWaitWake(b *testing.B) {
	server := benchmarkServer(b)
	changed := make(chan struct{})
	close(changed)
	server.wait(changed)
	b.ReportAllocs()
	for b.Loop() {
		server.wait(changed)
	}
}

func TestSteadyStateHandlerAllocations(t *testing.T) {
	server := newTestServer(t, t.TempDir())
	if err := server.source.Publish(validScene); err != nil {
		t.Fatal(err)
	}
	handler := server.app.Handler()
	for _, path := range []string{"/state?after=0", "/", "/assets/main.js"} {
		t.Run(path, func(t *testing.T) {
			var request fasthttp.Request
			request.SetRequestURI(sourceOrigin + path)
			request.Header.SetMethod("GET")
			request.Header.Set("Authorization", server.source.current.Load().bearer)
			var ctx fasthttp.RequestCtx
			ctx.Init(&request, &net.TCPAddr{}, nil)
			handler(&ctx)
			allocations := testing.AllocsPerRun(1000, func() { ctx.Response.Reset(); handler(&ctx) })
			if ctx.Response.StatusCode() != 200 {
				t.Fatalf("status %d", ctx.Response.StatusCode())
			}
			if allocations != 0 {
				t.Fatalf("steady-state handler allocated %g times per request", allocations)
			}
		})
	}
}
