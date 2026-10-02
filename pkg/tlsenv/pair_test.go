// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tlsenv_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ItsBagelBot/pkg/tlsenv"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	certVar = tlsenv.Var("TEST_TLS_CERT_FILE")
	keyVar  = tlsenv.Var("TEST_TLS_KEY_FILE")
)

type keyFiles struct{ cert, key string }

func writePair(t *testing.T, dir string, serial int64) keyFiles {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "tlsenv-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	files := keyFiles{cert: filepath.Join(dir, "tls.crt"), key: filepath.Join(dir, "tls.key")}
	require.NoError(t, os.WriteFile(files.cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(files.key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))
	return files
}

func pairFromEnv(t *testing.T, cert, key string) (tlsenv.Pair, error) {
	t.Helper()
	t.Setenv(string(certVar), cert)
	t.Setenv(string(keyVar), key)
	return tlsenv.PairFromEnv(certVar, keyVar)
}

func serialOf(t *testing.T, cert *tls.Certificate) int64 {
	t.Helper()
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	return leaf.SerialNumber.Int64()
}

func TestPairFromEnvLoadsTheConfiguredFiles(t *testing.T) {
	tests := []struct {
		name    string
		asMount func(path string) string
	}{
		{name: "loads exact paths", asMount: func(path string) string { return path }},
		{name: "loads paths padded by mounts", asMount: func(path string) string { return "  " + path + "\n" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			written := writePair(t, t.TempDir(), 1)

			pair, err := pairFromEnv(t, tc.asMount(written.cert), tc.asMount(written.key))

			require.NoError(t, err)
			assert.True(t, pair.Configured())
			assert.Equal(t, written.cert, pair.CertFile())
			assert.Equal(t, written.key, pair.KeyFile())
			cert, err := pair.Load()
			require.NoError(t, err)
			assert.Equal(t, int64(1), serialOf(t, cert))
		})
	}
}

func TestPairFromEnvWithNothingSetIsUnconfigured(t *testing.T) {
	pair, err := pairFromEnv(t, "", "")

	require.NoError(t, err)
	assert.False(t, pair.Configured())
	assert.Empty(t, pair.CertFile())
	assert.Empty(t, pair.KeyFile())
	cfg, err := pair.ServerConfig()
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestPairFromEnvHalfSetIsAnError(t *testing.T) {
	tests := []struct{ name, cert, key string }{
		{name: "rejects only cert set", cert: "/etc/svc/tls/some.pem"},
		{name: "rejects only key set", key: "/etc/svc/tls/some.pem"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pair, err := pairFromEnv(t, tc.cert, tc.key)

			require.Error(t, err)
			assert.ErrorContains(t, err, "must both be set or both empty")
			assert.ErrorContains(t, err, string(certVar))
			assert.ErrorContains(t, err, string(keyVar))
			assert.False(t, pair.Configured(), "a rejected pair must not look configured")
		})
	}
}

func TestUnreadableFilesErrorEverywhere(t *testing.T) {
	dir := t.TempDir()
	pair, err := pairFromEnv(t, filepath.Join(dir, "missing.crt"), filepath.Join(dir, "missing.key"))
	require.NoError(t, err, "PairFromEnv checks configuration, not the filesystem")

	tests := []struct {
		name string
		load func() error
	}{
		{name: "Load fails", load: func() error { _, err := pair.Load(); return err }},
		{name: "GetCertificate fails", load: func() error { _, err := pair.GetCertificate(nil); return err }},
		{name: "GetClientCertificate fails", load: func() error { _, err := pair.GetClientCertificate(nil); return err }},
		{name: "ServerConfig fails", load: func() error { _, err := pair.ServerConfig(); return err }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorContains(t, tc.load(), "tlsenv: load key pair")
		})
	}
}

func TestClosuresRereadRotatedPair(t *testing.T) {
	dir := t.TempDir()
	written := writePair(t, dir, 1)
	pair, err := pairFromEnv(t, written.cert, written.key)
	require.NoError(t, err)

	cert, err := pair.GetCertificate(nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), serialOf(t, cert))

	writePair(t, dir, 2)

	cert, err = pair.GetCertificate(nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), serialOf(t, cert), "server handshake must re-read from disk")

	cert, err = pair.GetClientCertificate(nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), serialOf(t, cert), "client handshake must re-read from disk")
}

func presentedSerial(t *testing.T, addr string) int64 {
	t.Helper()

	// codeql[go/disabled-certificate-check] -- test client dialing a self-signed pair.
	conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	return conn.ConnectionState().PeerCertificates[0].SerialNumber.Int64()
}

func TestServerConfigPresentsRotatedCert(t *testing.T) {
	dir := t.TempDir()
	written := writePair(t, dir, 1)
	pair, err := pairFromEnv(t, written.cert, written.key)
	require.NoError(t, err)

	cfg, err := pair.ServerConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{Handler: http.NotFoundHandler(), TLSConfig: cfg}
	t.Cleanup(func() { _ = srv.Close() })
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	addr := ln.Addr().String()
	require.Equal(t, int64(1), presentedSerial(t, addr))

	writePair(t, dir, 2)
	assert.Equal(t, int64(2), presentedSerial(t, addr),
		"the same running listener must present the rotated cert")
}
