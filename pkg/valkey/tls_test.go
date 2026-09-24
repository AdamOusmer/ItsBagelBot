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
		assert.ErrorContains(t, err, "must both be set or both empty")
	})

	t.Run("only key set", func(t *testing.T) {
		t.Setenv("VALKEY_TLS_CLIENT_CERT_FILE", "")
		t.Setenv("VALKEY_TLS_CLIENT_KEY_FILE", "/dev/null")

		_, err := clientTLSConfig()
		assert.ErrorContains(t, err, "must both be set or both empty")
	})
}

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

	presented := doTestHandshake(t, clientConfig, serverConfig)
	assert.Equal(t, certV1.SerialNumber, presented.SerialNumber, "first handshake should present the cert loaded at construction")
	assert.Equal(t, "client-v1", presented.Subject.CommonName)

	_, _, certV2 := writeTestClientCertAt(t, ca, caKey, certFile, keyFile, "client-v2", 2)
	futureTime := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(certFile, futureTime, futureTime))

	presented = doTestHandshake(t, clientConfig, serverConfig)
	assert.Equal(t, certV2.SerialNumber, presented.SerialNumber, "second handshake must present the ROTATED cert, not the one cached at startup")
	assert.Equal(t, "client-v2", presented.Subject.CommonName)
	assert.NotEqual(t, certV1.SerialNumber, presented.SerialNumber)
}

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

func TestClientCertReloaderKeepsServingCachedCertOnReloadFailure(t *testing.T) {
	ca, caKey := testTLSCA(t)
	certFile, keyFile, certV1 := writeTestClientCert(t, ca, caKey, "client-v1", 1)

	reloader, err := newClientCertReloader(certFile, keyFile)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(certFile, []byte("not a certificate"), 0o600))
	futureTime := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(certFile, futureTime, futureTime))

	cert, err := reloader.getClientCertificate(nil)
	require.NoError(t, err, "a broken rotated file must not fail the handshake")
	require.NotNil(t, cert.Leaf)
	assert.Equal(t, certV1.SerialNumber, cert.Leaf.SerialNumber, "must keep serving the last good cert")
}

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

func writeTestClientCert(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, cn string, serial int64) (certFile, keyFile string, cert *x509.Certificate) {
	t.Helper()
	dir := t.TempDir()
	return writeTestClientCertAt(t, ca, caKey, filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"), cn, serial)
}

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
