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
	written := writePair(t, t.TempDir(), 1)
	t.Setenv(string(certVar), written.CertFile())
	t.Setenv(string(keyVar), written.KeyFile())

	pair, err := PairFromEnv(certVar, keyVar)
	require.NoError(t, err)
	assert.True(t, pair.Configured())
	assert.Equal(t, written, pair)

	cert, err := pair.Load()
	require.NoError(t, err)
	assert.Equal(t, int64(1), serialOf(t, cert))
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
