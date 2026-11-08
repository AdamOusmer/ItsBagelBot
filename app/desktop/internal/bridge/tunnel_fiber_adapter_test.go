// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bridge

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"strings"
)

// probeUpgradeHandler restores net/http's immediate 101 behavior around the
// fasthttp 1.75 adaptor, which otherwise discards buffered upgrade headers when
// Hijack sets HijackSetNoResponse. It does not implement WebSocket framing.
func probeUpgradeHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// fasthttp's RequestCtx is pooled after hijacking. A native WebSocket
		// lifecycle must not retain it as its context parent; the probe adds its
		// independent server cancellation and CloseRead observes socket closure.
		request := ownProbeRequest(r)
		next.ServeHTTP(&probeUpgradeWriter{ResponseWriter: w, headers: make(http.Header)}, request)
	})
}

// ConvertRequest exposes strings backed by fasthttp's pooled request storage.
// Clone owns the header containers but not those strings, so copy the fields
// the native upgrade handler or its long-lived call could retain.
func ownProbeRequest(source *http.Request) *http.Request {
	request := source.Clone(context.Background())
	request.Method = strings.Clone(source.Method)
	request.Proto = strings.Clone(source.Proto)
	request.Host = strings.Clone(source.Host)
	request.RequestURI = strings.Clone(source.RequestURI)
	request.Header = make(http.Header, len(source.Header))
	for name, values := range source.Header {
		owned := make([]string, len(values))
		for i, value := range values {
			owned[i] = strings.Clone(value)
		}
		request.Header[strings.Clone(name)] = owned
	}
	if request.URL != nil {
		request.URL.Path = strings.Clone(source.URL.Path)
		request.URL.RawPath = strings.Clone(source.URL.RawPath)
		request.URL.RawQuery = strings.Clone(source.URL.RawQuery)
	}
	return request
}

type probeUpgradeWriter struct {
	http.ResponseWriter
	headers http.Header
	status  int
}

func (w *probeUpgradeWriter) Header() http.Header { return w.headers }
func (w *probeUpgradeWriter) WriteHeader(status int) {
	w.status = status
	if status == http.StatusSwitchingProtocols {
		return
	}
	for key, values := range w.headers {
		w.ResponseWriter.Header()[key] = values
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *probeUpgradeWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}
func (w *probeUpgradeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := w.ResponseWriter.(http.Hijacker).Hijack()
	if err != nil {
		return nil, nil, err
	}
	response := http.Response{StatusCode: w.status, ProtoMajor: 1, ProtoMinor: 1, Header: w.headers}
	if err = response.Write(buffer.Writer); err == nil {
		err = buffer.Flush()
	}
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return conn, buffer, nil
}
