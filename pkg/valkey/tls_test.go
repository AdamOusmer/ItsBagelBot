// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClientTLSConfigGateBothOrNeither pins the existing env gate: this
// change must not disturb it. Unset stays today's "no client auth yet"
// behavior; exactly one set is still a startup error.
func TestClientTLSConfigGateBothOrNeither(t *testing.T) {
	ca, _ := testTLSCA(t)
	t.Setenv("VALKEY_TLS_CA_PEM", string(pemEncodeCert(ca)))

	t.Run("neither set", func(t *testing.T) {
		t.Setenv("VALKEY_TLS_CLIENT_CERT_FILE", "")
		t.Setenv("VALKEY_TLS_CLIENT_KEY_FILE", "")

		config, err := clientTLSConfig()
		require.NoError(t, err)
		require.NotNil(t, config)
		assert.Nil(t, config.GetClientCertificate)
		assert.Empty(t, config.Certificates)
	})

	t.Run("only cert set", func(t *testing.T) {
		t.Setenv("VALKEY_TLS_CLIENT_CERT_FILE", "/dev/null")
		t.Setenv("VALKEY_TLS_CLIENT_KEY_FILE", "")

		_, err := clientTLSConfig()
		assert.ErrorContains(t, err, "must both be set or both be empty")
	})

	t.Run("only key set", func(t *testing.T) {
		t.Setenv("VALKEY_TLS_CLIENT_CERT_FILE", "")
		t.Setenv("VALKEY_TLS_CLIENT_KEY_FILE", "/dev/null")

		_, err := clientTLSConfig()
		assert.ErrorContains(t, err, "must both be set or both be empty")
	})
}

// TestClientTLSConfigUsesGetClientCertificateNotStaticCertificates guards the
// actual bug fix: Certificates is a startup-only snapshot, GetClientCertificate
// is called by crypto/tls on every handshake. Regressing to the former is the
// exact shape of #560.
func TestClientTLSConfigUsesGetClientCertificateNotStaticCertificates(t *testing.T) {
	ca, caKey := testTLSCA(t)
	certFile, keyFile, _ := writeTestClientCert(t, ca, caKey, "client-v1", 1)

	t.Setenv("VALKEY_TLS_CLIENT_CERT_FILE", certFile)
	t.Setenv("VALKEY_TLS_CLIENT_KEY_FILE", keyFile)
	t.Setenv("VALKEY_TLS_CA_PEM", string(pemEncodeCert(ca)))

	config, err := clientTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, config.GetClientCertificate)
	assert.Empty(t, config.Certificates, "Certificates must stay unset: a non-empty value here is read once at Config construction and never again")
}

// TestClientCertReloaderPresentsRotatedCertOnNextHandshake is the point of
// this change: cert-manager rotates the file on disk at day 75 without the
// process restarting, and the very next handshake -- not a redeploy -- must
// present the new cert. This drives two real TLS handshakes end to end over
// a net.Pipe and inspects what the server actually received.
func TestClientCertReloaderPresentsRotatedCertOnNextHandshake(t *testing.T) {
	ca, caKey := testTLSCA(t)
	certFile, keyFile, certV1 := writeTestClientCert(t, ca, caKey, "client-v1", 1)

	clientConfig := &tls.Config{
		ServerName: "valkey-test.local",
		RootCAs:    certPoolOf(ca),
		MinVersion: tls.VersionTLS12,
	}
	reloader, err := newClientCertReloader(certFile, keyFile)
	require.NoError(t, err)
	clientConfig.GetClientCertificate = reloader.getClientCertificate

	serverConfig := testServerTLSConfig(t, ca, caKey)

	// Handshake #1: reloader has only ever seen v1.
	presented := doTestHandshake(t, clientConfig, serverConfig)
	assert.Equal(t, certV1.SerialNumber, presented.SerialNumber, "first handshake should present the cert loaded at construction")
	assert.Equal(t, "client-v1", presented.Subject.CommonName)

	// cert-manager's renewal: the Secret volume's atomic symlink swap lands
	// a same-path, different-content pair with a fresh mtime.
	_, _, certV2 := writeTestClientCertAt(t, ca, caKey, certFile, keyFile, "client-v2", 2)
	futureTime := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(certFile, futureTime, futureTime))

	// Handshake #2: no restart, same *tls.Config, same reloader -- must pick
	// up the rotated cert this time.
	presented = doTestHandshake(t, clientConfig, serverConfig)
	assert.Equal(t, certV2.SerialNumber, presented.SerialNumber, "second handshake must present the ROTATED cert, not the one cached at startup")
	assert.Equal(t, "client-v2", presented.Subject.CommonName)
	assert.NotEqual(t, certV1.SerialNumber, presented.SerialNumber)
}

// TestClientCertReloaderCachesWhenFileUnchanged proves the reloader does not
// stat-and-reparse the key pair on every handshake: back-to-back calls
// against an untouched file must return the exact same cached *tls.Certificate.
func TestClientCertReloaderCachesWhenFileUnchanged(t *testing.T) {
	ca, caKey := testTLSCA(t)
	certFile, keyFile, _ := writeTestClientCert(t, ca, caKey, "client-v1", 1)

	reloader, err := newClientCertReloader(certFile, keyFile)
	require.NoError(t, err)

	first, err := reloader.getClientCertificate(nil)
	require.NoError(t, err)
	second, err := reloader.getClientCertificate(nil)
	require.NoError(t, err)

	assert.Same(t, first, second, "unchanged file must not trigger a reparse")
}

// TestClientCertReloaderKeepsServingCachedCertOnReloadFailure covers the
// required degrade path: a rotated file that's briefly invalid (mid-write,
// truncated) must not fail a live handshake. The last good cert keeps serving
// until a subsequent, successful reload replaces it.
func TestClientCertReloaderKeepsServingCachedCertOnReloadFailure(t *testing.T) {
	ca, caKey := testTLSCA(t)
	certFile, keyFile, certV1 := writeTestClientCert(t, ca, caKey, "client-v1", 1)

	reloader, err := newClientCertReloader(certFile, keyFile)
	require.NoError(t, err)

	// Corrupt the cert file in place and bump mtime/size so changed() fires.
	require.NoError(t, os.WriteFile(certFile, []byte("not a certificate"), 0o600))
	futureTime := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(certFile, futureTime, futureTime))

	cert, err := reloader.getClientCertificate(nil)
	require.NoError(t, err, "a broken rotated file must not fail the handshake")
	require.NotNil(t, cert.Leaf)
	assert.Equal(t, certV1.SerialNumber, cert.Leaf.SerialNumber, "must keep serving the last good cert")
}

// --- test fixtures -----------------------------------------------------

func testTLSCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Valkey_Test_CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert, key
}

func certPoolOf(certs ...*x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	for _, cert := range certs {
		pool.AddCert(cert)
	}
	return pool
}

func pemEncodeCert(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

// writeTestClientCert issues a fresh ca-signed client-auth cert/key pair
// under CommonName cn and serial number, and writes them to a new temp dir.
func writeTestClientCert(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, cn string, serial int64) (certFile, keyFile string, cert *x509.Certificate) {
	t.Helper()
	dir := t.TempDir()
	return writeTestClientCertAt(t, ca, caKey, filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"), cn, serial)
}

// writeTestClientCertAt issues a fresh cert/key pair signed by ca and writes
// it to an EXISTING path, standing in for cert-manager rewriting the same
// file the reloader is already watching.
func writeTestClientCertAt(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, certFile, keyFile, cn string, serial int64) (certFileOut, keyFileOut string, cert *x509.Certificate) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, ca, &key.PublicKey, caKey)
	require.NoError(t, err)
	cert, err = x509.ParseCertificate(der)
	require.NoError(t, err)

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))

	return certFile, keyFile, cert
}

// testServerTLSConfig builds a Valkey-stand-in server config that requires
// and verifies a client certificate against ca, mirroring tls-auth-clients.
func testServerTLSConfig(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey) *tls.Config {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := x509.Certificate{
		SerialNumber:          big.NewInt(99),
		Subject:               pkix.Name{CommonName: "valkey-test-server"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		DNSNames:              []string{"valkey-test.local"},
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, ca, &key.PublicKey, caKey)
	require.NoError(t, err)

	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPoolOf(ca),
		MinVersion:   tls.VersionTLS12,
	}
}

// doTestHandshake runs one real client/server TLS handshake over an in-memory
// pipe and returns the certificate the server actually received from the
// client -- the same thing crypto/tls would send over the wire to Valkey.
func doTestHandshake(t *testing.T, clientConfig, serverConfig *tls.Config) *x509.Certificate {
	t.Helper()

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	clientTLS := tls.Client(clientConn, clientConfig)
	serverTLS := tls.Server(serverConn, serverConfig)

	clientErr := make(chan error, 1)
	go func() { clientErr <- clientTLS.Handshake() }()

	require.NoError(t, serverTLS.Handshake())
	require.NoError(t, <-clientErr)

	peers := serverTLS.ConnectionState().PeerCertificates
	require.NotEmpty(t, peers, "server must have received a client certificate")
	return peers[0]
}
