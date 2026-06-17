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

	name, err := registerTLS(" mysql.internal ", nil)
	require.NoError(t, err)
	require.Equal(t, tlsConfigName, name)

	cfg := registeredTLSConfig(t, "10.0.0.4:3306")
	require.False(t, cfg.InsecureSkipVerify)
	require.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	require.Equal(t, "mysql.internal", cfg.ServerName)
	require.Nil(t, cfg.RootCAs)
}

func TestRegisterTLSDerivesServerNameFromAddress(t *testing.T) {
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	_, err := registerTLS("", nil)
	require.NoError(t, err)

	cfg := registeredTLSConfig(t, "db.example.com:3306")
	require.False(t, cfg.InsecureSkipVerify)
	require.Equal(t, "db.example.com", cfg.ServerName)
}

func TestRegisterTLSUsesCustomCA(t *testing.T) {
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	_, err := registerTLS("", testCAPEM(t))
	require.NoError(t, err)

	cfg := registeredTLSConfig(t, "db.example.com:3306")
	require.False(t, cfg.InsecureSkipVerify)
	require.NotNil(t, cfg.RootCAs)
}

func TestRegisterTLSRejectsInvalidCA(t *testing.T) {
	_, err := registerTLS("", []byte("not pem"))
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

func testCAPEM(t *testing.T) []byte {
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

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
