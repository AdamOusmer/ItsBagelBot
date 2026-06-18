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

func TestRegisterTLSUsesVerifiedConfig(t *testing.T) {
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	name, err := registerTLS(nil)
	require.NoError(t, err)
	require.Equal(t, tlsConfigName, name)

	cfg := registeredTLSConfig(t, "10.0.0.4:3306")
	require.True(t, cfg.InsecureSkipVerify)
	require.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	require.Empty(t, cfg.ServerName)
	require.Nil(t, cfg.RootCAs)
	require.Nil(t, cfg.VerifyPeerCertificate)
}

func TestRegisterTLSUsesPinnedCAWithoutHostnameVerification(t *testing.T) {
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	ca, caKey, caPEM := testCA(t)
	_, err := registerTLS(caPEM)
	require.NoError(t, err)

	cfg := registeredTLSConfig(t, "10.0.0.4:3306")
	require.True(t, cfg.InsecureSkipVerify)
	require.Empty(t, cfg.ServerName)
	require.NotNil(t, cfg.RootCAs)
	require.NotNil(t, cfg.VerifyPeerCertificate)

	require.NoError(t, cfg.VerifyPeerCertificate(testLeafChain(t, ca, caKey), nil))
}

func TestRegisterTLSRejectsUntrustedServerCertificate(t *testing.T) {
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	_, _, trustedCAPEM := testCA(t)
	untrustedCA, untrustedKey, _ := testCA(t)

	_, err := registerTLS(trustedCAPEM)
	require.NoError(t, err)

	cfg := registeredTLSConfig(t, "db.example.com:3306")
	require.Error(t, cfg.VerifyPeerCertificate(testLeafChain(t, untrustedCA, untrustedKey), nil))
}

func TestRegisterTLSRejectsMissingServerCertificate(t *testing.T) {
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	_, _, caPEM := testCA(t)
	_, err := registerTLS(caPEM)
	require.NoError(t, err)

	cfg := registeredTLSConfig(t, "db.example.com:3306")
	require.ErrorContains(t, cfg.VerifyPeerCertificate(nil, nil), "server presented no certificate")
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

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
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

	return cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func testLeafChain(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey) [][]byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber:          big.NewInt(2),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, ca, &key.PublicKey, caKey)
	require.NoError(t, err)

	return [][]byte{der}
}
