// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const testDBAddr = "10.0.0.4:3306"

func TestNewDriverRejectsUnusableTLSConfiguration(t *testing.T) {
	_, _, caPEM := testCA(t)
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "rejects a missing pinned CA", wantErr: "DB_CA_CERT is required"},
		{name: "rejects a blank pinned CA", env: map[string]string{"DB_CA_CERT": " \n\t"}, wantErr: "DB_CA_CERT is required"},
		{name: "rejects a non-PEM pinned CA", env: map[string]string{"DB_CA_CERT": "not pem"}, wantErr: "DB_CA_CERT did not contain a valid PEM certificate"},
		{name: "rejects a client cert without its key", env: map[string]string{"DB_CA_CERT": string(caPEM), "DB_CLIENT_CERT": "cert"}, wantErr: "must both be set or both empty"},
		{name: "rejects an unparsable client key pair", env: map[string]string{"DB_CA_CERT": string(caPEM), "DB_CLIENT_CERT": "cert", "DB_CLIENT_KEY": "key"}, wantErr: "client key pair"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			driver, err := NewDriver(Config{Address: testDBAddr, Schema: "test_schema"})

			require.ErrorContains(t, err, tc.wantErr)
			require.Nil(t, driver)
		})
	}
}

func TestNewDriverOpensPoolWhoseHealthProbeReportsAnUnreachableServer(t *testing.T) {
	_, _, caPEM := testCA(t)
	t.Setenv("DB_CA_CERT", string(caPEM))
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	driver, err := NewDriver(Config{Address: "127.0.0.1:1", Schema: "test_schema", MaxConns: 2})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, driver.Close()) })

	err = HealthCheck("mysql", driver.DB()).Probe(context.Background())

	require.ErrorContains(t, err, "db: keepalive")
}

const testDBHost = "db.internal.example"

func TestRegisterTLSPinsCAAndHostWithSessionResumption(t *testing.T) {
	ca, _, cfg := registerTestCA(t, testDBHost+":3306")
	pinnedOnly := x509.NewCertPool()
	pinnedOnly.AddCert(ca)

	type tlsSummary struct {
		minVersion         uint16
		serverName         string
		skipVerify         bool
		pinnedRootsOnly    bool
		noCustomVerify     bool
		clientSessionCache tls.ClientSessionCache
	}
	require.Equal(t,
		tlsSummary{tls.VersionTLS13, testDBHost, false, true, true, mysqlSessionCache},
		tlsSummary{
			minVersion:         cfg.MinVersion,
			serverName:         cfg.ServerName,
			skipVerify:         cfg.InsecureSkipVerify,
			pinnedRootsOnly:    cfg.RootCAs.Equal(pinnedOnly),
			noCustomVerify:     cfg.VerifyConnection == nil && cfg.VerifyPeerCertificate == nil,
			clientSessionCache: cfg.ClientSessionCache,
		})
}

func TestRegisterTLSRejectsAddrWithoutPort(t *testing.T) {
	_, _, caPEM := testCA(t)
	_, err := registerTLS(caPEM, testDBHost)
	require.ErrorContains(t, err, "must be host:port")
}

func TestRegisterTLSVerifiesServerAgainstPinnedCAAndHost(t *testing.T) {
	ca, caKey, cfg := registerTestCA(t, testDBHost+":3306")
	forgedCA, forgedKey := testNamedCA(t, "MySQL_Endpoint_CA")
	unrelatedCA, unrelatedKey := testNamedCA(t, "Unrelated_CA")

	tests := []struct {
		name      string
		issuer    *x509.Certificate
		issuerKey *ecdsa.PrivateKey
		dnsName   string
		wantErr   bool
	}{
		{name: "accepts a pinned CA leaf for the DB_ADDR host", issuer: ca, issuerKey: caKey, dnsName: testDBHost},
		{name: "rejects a hostname mismatch", issuer: ca, issuerKey: caKey, dnsName: "someone-else.example", wantErr: true},
		{name: "rejects an unrelated CA", issuer: unrelatedCA, issuerKey: unrelatedKey, dnsName: testDBHost, wantErr: true},
		{name: "rejects a forged CA copying the pinned subject", issuer: forgedCA, issuerKey: forgedKey, dnsName: testDBHost, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			leaf := testLeafWithSAN(t, tc.issuer, tc.issuerKey, tc.dnsName)

			_, err := leaf.Verify(x509.VerifyOptions{Roots: cfg.RootCAs, DNSName: cfg.ServerName})

			require.Equal(t, tc.wantErr, err != nil, "verify error: %v", err)
		})
	}
}

func TestWarnIfPinnedCANearExpiryFiresInsideWindow(t *testing.T) {
	logs := captureGlobalLogs(t)

	cert, _ := testNamedCAWithValidity(t, "MySQL_Endpoint_CA", time.Now().Add(10*24*time.Hour))
	warnIfPinnedCANearExpiry(cert)

	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	require.Contains(t, entry.Message, "DB_CA_CERT")
	require.Equal(t, zap.ErrorLevel, entry.Level)
}

func TestWarnIfPinnedCANearExpiryStaysQuietOutsideWindow(t *testing.T) {
	logs := captureGlobalLogs(t)

	cert, _ := testNamedCAWithValidity(t, "MySQL_Endpoint_CA", time.Now().Add(90*24*time.Hour))
	warnIfPinnedCANearExpiry(cert)

	require.Equal(t, 0, logs.Len())
}

func TestNewMySQLConfigSetsNetworkTimeouts(t *testing.T) {
	mc := newMySQLConfig(Config{Address: testDBAddr, Schema: "test_schema"})

	require.Equal(t, dialTimeout, mc.Timeout)
	require.Equal(t, readTimeout, mc.ReadTimeout)
	require.Equal(t, writeTimeout, mc.WriteTimeout)
	require.NotNil(t, mc.DialFunc)
}

func captureGlobalLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()

	core, logs := observer.New(zap.ErrorLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(restore)

	return logs
}

func registerTestCA(t *testing.T, addr string) (*x509.Certificate, *ecdsa.PrivateKey, *tls.Config) {
	t.Helper()
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	ca, caKey, caPEM := testCA(t)
	_, err := registerTLS(caPEM, addr)
	require.NoError(t, err)

	return ca, caKey, registeredTLSConfig(t, addr)
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

	cert, key := testNamedCA(t, "MySQL_Endpoint_CA")
	return cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

func testNamedCA(t *testing.T, commonName string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	return testNamedCAWithValidity(t, commonName, time.Now().Add(time.Hour))
}

func testNamedCAWithValidity(t *testing.T, commonName string, notAfter time.Time) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	return issueTestCertificate(t, template, nil, nil)
}

func testLeafWithSAN(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, dnsName string) *x509.Certificate {
	t.Helper()

	template := x509.Certificate{
		SerialNumber:          big.NewInt(2),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{dnsName},
	}

	cert, _ := issueTestCertificate(t, template, ca, caKey)
	return cert
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
