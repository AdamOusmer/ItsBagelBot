package db

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

func TestRegisterTLSRejectsMissingCA(t *testing.T) {
	name, err := registerTLS(nil)
	require.Empty(t, name)
	require.ErrorContains(t, err, "DB_CA_CERT is required")

	name, err = registerTLS([]byte(" \n\t"))
	require.Empty(t, name)
	require.ErrorContains(t, err, "DB_CA_CERT is required")
}

func TestRegisterTLSUsesPinnedCAWithoutHostnameVerification(t *testing.T) {
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	ca, caKey, caPEM := testCA(t)
	_, err := registerTLS(caPEM)
	require.NoError(t, err)

	cfg := registeredTLSConfig(t, "10.0.0.4:3306")
	require.True(t, cfg.InsecureSkipVerify)
	require.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	require.Empty(t, cfg.ServerName)
	require.NotNil(t, cfg.RootCAs)
	require.Nil(t, cfg.VerifyPeerCertificate)
	require.NotNil(t, cfg.VerifyConnection)

	require.NoError(t, cfg.VerifyConnection(tls.ConnectionState{
		PeerCertificates: testLeafChain(t, ca, caKey),
	}))
}

func TestRegisterTLSRejectsUntrustedServerCertificate(t *testing.T) {
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	_, _, trustedCAPEM := testCA(t)
	untrustedCA, untrustedKey, _ := testCA(t)

	_, err := registerTLS(trustedCAPEM)
	require.NoError(t, err)

	cfg := registeredTLSConfig(t, "db.example.com:3306")
	require.Error(t, cfg.VerifyConnection(tls.ConnectionState{
		PeerCertificates: testLeafChain(t, untrustedCA, untrustedKey),
	}))
}

func TestRegisterTLSRejectsMissingServerCertificate(t *testing.T) {
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	_, _, caPEM := testCA(t)
	_, err := registerTLS(caPEM)
	require.NoError(t, err)

	cfg := registeredTLSConfig(t, "db.example.com:3306")
	require.ErrorContains(t, cfg.VerifyConnection(tls.ConnectionState{}), "server presented no certificate")
}

func TestRegisterTLSRejectsInvalidCA(t *testing.T) {
	_, err := registerTLS([]byte("not pem"))
	require.ErrorContains(t, err, "DB_CA_CERT did not contain a valid PEM certificate")
}

func registeredTLSConfig(t *testing.T, addr string) *tls.Config {
	t.Helper()

	mc := mysql.NewConfig()
	mc.Net = "tcp"
	mc.Addr = addr
	mc.TLSConfig = tlsConfigName

	cfg, err := mysql.ParseDSN(mc.FormatDSN())
	require.NoError(t, err)
	require.NotNil(t, cfg.TLS)

	return cfg.TLS
}

func testCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	cert, key := issueTestCertificate(t, template, nil, nil)

	return cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

func testLeafChain(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey) []*x509.Certificate {
	t.Helper()

	template := x509.Certificate{
		SerialNumber:          big.NewInt(2),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	cert, _ := issueTestCertificate(t, template, ca, caKey)
	return []*x509.Certificate{cert}
}

func issueTestCertificate(
	t *testing.T,
	template x509.Certificate,
	parent *x509.Certificate,
	signer any,
) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	if parent == nil {
		parent = &template
		signer = key
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, parent, &key.PublicKey, signer)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return cert, key
}
