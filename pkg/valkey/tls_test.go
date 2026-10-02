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

type testPKI struct {
	ca    *x509.Certificate
	caKey *ecdsa.PrivateKey
}

type clientCertFiles struct {
	cert string
	key  string
}

func newTestPKI(t *testing.T) testPKI {
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
	ca, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return testPKI{ca: ca, caKey: key}
}

func (p testPKI) caPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: p.ca.Raw})
}

func (p testPKI) pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(p.ca)
	return pool
}

func (p testPKI) issue(t *testing.T, template x509.Certificate) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template.NotBefore = time.Now().Add(-time.Hour)
	template.NotAfter = time.Now().Add(time.Hour)
	template.KeyUsage = x509.KeyUsageDigitalSignature
	template.BasicConstraintsValid = true
	der, err := x509.CreateCertificate(rand.Reader, &template, p.ca, &key.PublicKey, p.caKey)
	require.NoError(t, err)
	return der, key
}

func (p testPKI) writeClientCert(t *testing.T, files clientCertFiles, serial int64) *x509.Certificate {
	t.Helper()
	der, key := p.issue(t, x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "client-v" + big.NewInt(serial).String()},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(files.cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(files.key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

func (p testPKI) serverConfig(t *testing.T) *tls.Config {
	t.Helper()
	der, key := p.issue(t, x509.Certificate{
		SerialNumber: big.NewInt(99),
		Subject:      pkix.Name{CommonName: "valkey-test-server"},
		DNSNames:     []string{"valkey-test.local"},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    p.pool(),
		MinVersion:   tls.VersionTLS12,
	}
}

func newClientCertFiles(t *testing.T) clientCertFiles {
	t.Helper()
	dir := t.TempDir()
	return clientCertFiles{cert: filepath.Join(dir, "tls.crt"), key: filepath.Join(dir, "tls.key")}
}

func touchInTheFuture(t *testing.T, path string) {
	t.Helper()
	future := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(path, future, future))
}

func handshakeClientCert(t *testing.T, clientConfig, serverConfig *tls.Config) *x509.Certificate {
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

func TestClientTLSConfigGateBothOrNeither(t *testing.T) {
	pki := newTestPKI(t)
	files := newClientCertFiles(t)
	pki.writeClientCert(t, files, 1)
	t.Setenv("VALKEY_TLS_CA_PEM", string(pki.caPEM()))

	for _, tc := range []struct {
		name         string
		certFile     string
		keyFile      string
		wantErr      string
		wantReloader bool
	}{
		{name: "neither set"},
		{name: "only cert set", certFile: "/dev/null", wantErr: "must both be set or both empty"},
		{name: "only key set", keyFile: "/dev/null", wantErr: "must both be set or both empty"},
		{name: "TestClientTLSConfigUsesGetClientCertificateNotStaticCertificates", certFile: files.cert, keyFile: files.key, wantReloader: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("VALKEY_TLS_CLIENT_CERT_FILE", tc.certFile)
			t.Setenv("VALKEY_TLS_CLIENT_KEY_FILE", tc.keyFile)

			config, err := clientTLSConfig()

			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, config)
			assert.Equal(t, tc.wantReloader, config.GetClientCertificate != nil)
			assert.Empty(t, config.Certificates, "Certificates must stay unset: a non-empty value here is read once at Config construction and never again")
		})
	}
}

func TestClientCertReloaderPresentsRotatedCertOnNextHandshake(t *testing.T) {
	pki := newTestPKI(t)
	files := newClientCertFiles(t)
	certV1 := pki.writeClientCert(t, files, 1)
	reloader, err := newClientCertReloader(files.cert, files.key)
	require.NoError(t, err)
	clientConfig := &tls.Config{
		ServerName:           "valkey-test.local",
		RootCAs:              pki.pool(),
		MinVersion:           tls.VersionTLS12,
		GetClientCertificate: reloader.getClientCertificate,
	}
	serverConfig := pki.serverConfig(t)

	first := handshakeClientCert(t, clientConfig, serverConfig)
	certV2 := pki.writeClientCert(t, files, 2)
	touchInTheFuture(t, files.cert)
	second := handshakeClientCert(t, clientConfig, serverConfig)

	assert.Equal(t, certV1.SerialNumber, first.SerialNumber, "first handshake should present the cert loaded at construction")
	assert.Equal(t, certV2.SerialNumber, second.SerialNumber, "second handshake must present the ROTATED cert, not the one cached at startup")
	assert.Equal(t, "client-v2", second.Subject.CommonName)
}

func TestClientCertReloaderCachesWhenFileUnchanged(t *testing.T) {
	pki := newTestPKI(t)
	files := newClientCertFiles(t)
	pki.writeClientCert(t, files, 1)
	reloader, err := newClientCertReloader(files.cert, files.key)
	require.NoError(t, err)

	first, err := reloader.getClientCertificate(nil)
	require.NoError(t, err)
	second, err := reloader.getClientCertificate(nil)
	require.NoError(t, err)

	assert.Same(t, first, second, "unchanged file must not trigger a reparse")
}

func TestClientCertReloaderKeepsServingCachedCertOnReloadFailure(t *testing.T) {
	pki := newTestPKI(t)
	files := newClientCertFiles(t)
	certV1 := pki.writeClientCert(t, files, 1)
	reloader, err := newClientCertReloader(files.cert, files.key)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(files.cert, []byte("not a certificate"), 0o600))
	touchInTheFuture(t, files.cert)

	cert, err := reloader.getClientCertificate(nil)

	require.NoError(t, err, "a broken rotated file must not fail the handshake")
	require.NotNil(t, cert.Leaf)
	assert.Equal(t, certV1.SerialNumber, cert.Leaf.SerialNumber, "must keep serving the last good cert")
}
