// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tlsenv

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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	certVar = Var("TEST_TLS_CERT_FILE")
	keyVar  = Var("TEST_TLS_KEY_FILE")
)

// writePair writes a self-signed cert/key pair carrying serial into dir, at the
// same two paths every time, so a second call is the shape of a cert-manager
// renewal: same paths, new content.
func writePair(t *testing.T, dir string, serial int64) Pair {
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

	pair := Pair{certFile: filepath.Join(dir, "tls.crt"), keyFile: filepath.Join(dir, "tls.key")}
	require.NoError(t, os.WriteFile(pair.certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(pair.keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))
	return pair
}

func serialOf(t *testing.T, cert *tls.Certificate) int64 {
	t.Helper()
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	return leaf.SerialNumber.Int64()
}

func TestPairFromEnvBothSet(t *testing.T) {

	// The padded case is not hypothetical: these values arrive from Doppler
	// and from Secret volumes, both of which carry a trailing newline, and an
	// untrimmed path fails as an ENOENT on a file that is plainly there.
	for name, asMounted := range map[string]func(string) string{
		"exact":            func(path string) string { return path },
		"padded by mounts": func(path string) string { return "  " + path + "\n" },
	} {
		t.Run(name, func(t *testing.T) {
			written := writePair(t, t.TempDir(), 1)
			t.Setenv(string(certVar), asMounted(written.CertFile()))
			t.Setenv(string(keyVar), asMounted(written.KeyFile()))

			pair, err := PairFromEnv(certVar, keyVar)
			require.NoError(t, err)
			assert.True(t, pair.Configured())
			assert.Equal(t, written, pair)

			cert, err := pair.Load()
			require.NoError(t, err)
			assert.Equal(t, int64(1), serialOf(t, cert))
		})
	}
}

// Neither set is the pre-rollout state of every service that has not been given
// its cert volume yet: no error, no TLS, not a half-configured listener.
func TestPairFromEnvNeitherSet(t *testing.T) {
	t.Setenv(string(certVar), "")
	t.Setenv(string(keyVar), "")

	pair, err := PairFromEnv(certVar, keyVar)
	require.NoError(t, err)
	assert.False(t, pair.Configured())
	assert.Empty(t, pair.CertFile())
	assert.Empty(t, pair.KeyFile())
}

func TestPairFromEnvHalfSetIsAnError(t *testing.T) {
	for name, set := range map[string]Var{"only cert set": certVar, "only key set": keyVar} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(string(certVar), "")
			t.Setenv(string(keyVar), "")
			t.Setenv(string(set), "/etc/svc/tls/some.pem")

			pair, err := PairFromEnv(certVar, keyVar)
			require.Error(t, err)
			assert.ErrorContains(t, err, "must both be set or both empty")
			assert.ErrorContains(t, err, string(certVar))
			assert.ErrorContains(t, err, string(keyVar))
			assert.False(t, pair.Configured(), "a rejected pair must not look configured")
		})
	}
}

// An unreadable pair must surface as an error from every entry point, never as
// a nil certificate a handshake would present as "no cert".
func TestUnreadableFileErrorsEverywhere(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(string(certVar), filepath.Join(dir, "missing.crt"))
	t.Setenv(string(keyVar), filepath.Join(dir, "missing.key"))

	pair, err := PairFromEnv(certVar, keyVar)
	require.NoError(t, err, "PairFromEnv checks configuration, not the filesystem")

	for name, load := range map[string]func() (*tls.Certificate, error){
		"Load":                 func() (*tls.Certificate, error) { return pair.Load() },
		"GetCertificate":       func() (*tls.Certificate, error) { return pair.GetCertificate(nil) },
		"GetClientCertificate": func() (*tls.Certificate, error) { return pair.GetClientCertificate(nil) },
	} {
		t.Run(name, func(t *testing.T) {
			cert, err := load()
			assert.Nil(t, cert)
			assert.ErrorContains(t, err, "tlsenv: load key pair")
		})
	}
}

// The point of the closures: cert-manager rotates the files in place at day 75
// and the very next handshake, with no restart, must present the new cert.
func TestClosuresRereadRotatedPair(t *testing.T) {
	dir := t.TempDir()
	written := writePair(t, dir, 1)
	t.Setenv(string(certVar), written.CertFile())
	t.Setenv(string(keyVar), written.KeyFile())

	pair, err := PairFromEnv(certVar, keyVar)
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

// serveOn starts an HTTPS listener on a loopback port with cfg and returns its
// address. The whole point of ServerConfig is what a real handshake presents,
// which only a real listener can show.
func serveOn(t *testing.T, cfg *tls.Config) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{Handler: http.NotFoundHandler(), TLSConfig: cfg}
	t.Cleanup(func() { _ = srv.Close() })
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	return ln.Addr().String()
}

// presentedSerial dials addr and reports the serial of the leaf the server
// presented. Verification is off because the pair is self-signed and the
// identity is not what is under test; the serial is read straight off the
// handshake, which is the assertion.
func presentedSerial(t *testing.T, addr string) int64 {
	t.Helper()

	// codeql[go/disabled-certificate-check] -- test client, see above.
	conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	leaf := conn.ConnectionState().PeerCertificates[0]
	return leaf.SerialNumber.Int64()
}

// The listener contract: cert-manager rewrites the mounted files in place and
// the next handshake must present the new cert, with no restart. This is what
// ListenAndServeTLS(certFile, keyFile) cannot do, and why every listener here
// serves ServerConfig with empty file names instead.
func TestServerConfigPresentsRotatedCert(t *testing.T) {
	dir := t.TempDir()
	pair := writePair(t, dir, 1)

	cfg, err := pair.ServerConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)

	addr := serveOn(t, cfg)
	require.Equal(t, int64(1), presentedSerial(t, addr))

	writePair(t, dir, 2)
	assert.Equal(t, int64(2), presentedSerial(t, addr),
		"the same running listener must present the rotated cert")
}

// An unconfigured pair is the pre-rollout state: no config, so the caller
// serves plaintext rather than a listener with no certificate.
func TestServerConfigUnconfiguredIsNil(t *testing.T) {
	cfg, err := Pair{}.ServerConfig()
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

// An unreadable pair must kill the boot rather than build a listener that only
// fails once something handshakes with it.
func TestServerConfigUnreadablePairErrors(t *testing.T) {
	dir := t.TempDir()
	pair := Pair{certFile: filepath.Join(dir, "missing.crt"), keyFile: filepath.Join(dir, "missing.key")}

	cfg, err := pair.ServerConfig()
	assert.Nil(t, cfg)
	assert.ErrorContains(t, err, "tlsenv: load key pair")
}
