// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package testkit

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
)

// ProxyRequest contains observations of the synthetic test request only. It is
// not production telemetry; fixtures must use disposable test credentials.
type ProxyRequest struct {
	ContentType   string
	Authorization string
	ALPN          string
	HTTPMajor     int
}

// ReverseProxy is a generic HTTP/2 TLS-terminating test hop. It is not a
// Cloudflare emulator: it exercises real streaming, framing and trailer
// forwarding over independently verified downstream and upstream TLS legs.
type ReverseProxy struct {
	Endpoint      string
	TLSConfig     *tls.Config
	server        *httptest.Server
	transport     *http.Transport
	count         atomic.Int64
	upstreamHTTP2 atomic.Bool
	requests      chan ProxyRequest
	once          sync.Once
}

func Proxy(t testing.TB, origin *Server) *ReverseProxy {
	t.Helper()
	target, err := url.Parse(origin.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{ForceAttemptHTTP2: true, TLSClientConfig: origin.TLSConfig.Clone()}
	forward := httputil.NewSingleHostReverseProxy(target)
	forward.Transport = transport
	forward.FlushInterval = -1
	proxy := &ReverseProxy{transport: transport, requests: make(chan ProxyRequest, 16)}
	forward.ModifyResponse = func(response *http.Response) error {
		proxy.upstreamHTTP2.Store(response.TLS != nil && response.TLS.NegotiatedProtocol == "h2")
		return preserveGRPCTrailers(response)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy.observe(r)
		forward.ServeHTTP(w, r)
	})
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			proxy.count.Add(1)
		}
	}
	server.StartTLS()
	certificate, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	proxy.Endpoint = server.URL
	proxy.TLSConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}
	proxy.server = server
	t.Cleanup(proxy.Close)
	return proxy
}
func (p *ReverseProxy) observe(request *http.Request) {
	observation := ProxyRequest{ContentType: request.Header.Get("Content-Type"), Authorization: request.Header.Get("Authorization"), HTTPMajor: request.ProtoMajor}
	if request.TLS != nil {
		observation.ALPN = request.TLS.NegotiatedProtocol
	}
	select {
	case p.requests <- observation:
	default:
	}
}
func (p *ReverseProxy) Requests() <-chan ProxyRequest { return p.requests }
func (p *ReverseProxy) PhysicalConnections() int64    { return p.count.Load() }
func (p *ReverseProxy) Close() {
	p.once.Do(func() { p.server.CloseClientConnections(); p.server.Close(); p.transport.CloseIdleConnections() })
}

// A native gRPC server may send terminal status in a trailers-only HEADERS
// frame. net/http otherwise exposes it as ordinary response headers, which a
// streaming reverse proxy could flush before END_STREAM and lose its meaning.
func preserveGRPCTrailers(response *http.Response) error {
	if response.Trailer == nil {
		response.Trailer = make(http.Header)
	}
	for _, key := range []string{"Grpc-Status", "Grpc-Message", "Grpc-Status-Details-Bin"} {
		if values := response.Header.Values(key); len(values) > 0 {
			response.Trailer[key] = values
			response.Header.Del(key)
		}
	}
	return nil
}

// UpstreamHTTP2 reports whether the last origin response negotiated TLS ALPN h2.
func (p *ReverseProxy) UpstreamHTTP2() bool { return p.upstreamHTTP2.Load() }
