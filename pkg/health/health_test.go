// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package health

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

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

// fakeConn records whether the probe spent a round trip, so the fast-path
// (cached state already false) is distinguishable from the heartbeat itself.
type fakeConn struct {
	connected bool
	flushErr  error
	flushed   bool
}

func (f *fakeConn) IsConnected() bool { return f.connected }

func (f *fakeConn) FlushTimeout(time.Duration) error {
	f.flushed = true
	return f.flushErr
}

func TestNATSHeartbeat(t *testing.T) {
	cases := []struct {
		name      string
		conn      *fakeConn
		ctx       context.Context
		wantErr   bool
		wantFlush bool
	}{
		{"disconnected fails without a round trip",
			&fakeConn{connected: false}, context.Background(), true, false},
		{"connected passes the round trip",
			&fakeConn{connected: true}, context.Background(), false, true},
		{"failed pong fails the check",
			&fakeConn{connected: true, flushErr: errors.New("i/o timeout")}, context.Background(), true, true},
		{"expired deadline fails before flushing",
			&fakeConn{connected: true}, expiredCtx(), true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NATS("nats", tc.conn).Probe(tc.ctx)
			if (err != nil) != tc.wantErr {
				t.Fatalf("probe err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.conn.flushed != tc.wantFlush {
				t.Fatalf("flushed = %v, want %v", tc.conn.flushed, tc.wantFlush)
			}
		})
	}
}

func TestNATSNilConnAlwaysPasses(t *testing.T) {
	if err := NATS("x", nil).Probe(context.Background()); err != nil {
		t.Fatalf("nil conn: %v", err)
	}
}

func expiredCtx() context.Context {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	cancel()
	return ctx
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

// writeKeyPair writes a fresh self-signed cert+key to dir and returns the
// paths plus the cert's serial, so rotation is observable.
func writeKeyPair(t *testing.T, dir string, serial int64) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"svc-status"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	certFile = filepath.Join(dir, "tls.crt")
	keyFile = filepath.Join(dir, "tls.key")
	writePEM(t, certFile, "CERTIFICATE", der)
	writePEM(t, keyFile, "EC PRIVATE KEY", keyDER)
	return certFile, keyFile
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func serialOf(t *testing.T, cfg *tls.Config) int64 {
	t.Helper()
	cert, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return leaf.SerialNumber.Int64()
}

// A cert-manager renewal swaps the mounted files in place; the listener must
// serve the new cert on the next handshake with no restart.
func TestTLSConfigReloadsRotatedCert(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeKeyPair(t, dir, 1)
	t.Setenv("TLS_CERT_FILE", certFile)
	t.Setenv("TLS_KEY_FILE", keyFile)

	cfg, err := tlsEnvConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := serialOf(t, cfg); got != 1 {
		t.Fatalf("serial = %d, want 1", got)
	}

	writeKeyPair(t, dir, 2)
	if got := serialOf(t, cfg); got != 2 {
		t.Fatalf("serial after rotation = %d, want 2 (must re-read from disk)", got)
	}
}

func TestServeRejectsHalfSetTLSPair(t *testing.T) {
	t.Setenv("TLS_CERT_FILE", "/etc/svc/tls/tls.crt")
	t.Setenv("TLS_KEY_FILE", "")

	select {
	case err := <-Serve("127.0.0.1:0", "svc"):
		if err == nil {
			t.Fatal("want error for half-set pair")
		}
	default:
		t.Fatal("want immediate error for half-set pair")
	}
}
