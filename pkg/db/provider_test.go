// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
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

func TestRegisterTLSRejectsMissingCA(t *testing.T) {
	name, err := registerTLS(nil, tlsModeVerifyCA, testDBAddr)
	require.Empty(t, name)
	require.ErrorContains(t, err, "DB_CA_CERT is required")

	name, err = registerTLS([]byte(" \n\t"), tlsModeVerifyCA, testDBAddr)
	require.Empty(t, name)
	require.ErrorContains(t, err, "DB_CA_CERT is required")
}

func TestRegisterTLSRejectsInvalidCA(t *testing.T) {
	_, err := registerTLS([]byte("not pem"), tlsModeVerifyCA, testDBAddr)
	require.ErrorContains(t, err, "DB_CA_CERT did not contain a valid PEM certificate")
}

func TestResolveTLSModeDefaultsToVerifyCA(t *testing.T) {
	mode, err := resolveTLSMode()
	require.NoError(t, err)
	require.Equal(t, tlsModeVerifyCA, mode)
}

func TestResolveTLSModeAcceptsVerifyIdentity(t *testing.T) {
	t.Setenv(tlsModeEnvVar, "VERIFY_IDENTITY")

	mode, err := resolveTLSMode()
	require.NoError(t, err)
	require.Equal(t, tlsModeVerifyIdentity, mode)
}

func TestResolveTLSModeRejectsUnknownValue(t *testing.T) {
	t.Setenv(tlsModeEnvVar, "TRUST_EVERYTHING")

	_, err := resolveTLSMode()
	require.ErrorContains(t, err, "DB_TLS_MODE")
}

// --- VERIFY_CA: the mode the fleet runs today against the OCI service-defined,
// SAN-less MySQL_Endpoint_CA. ---

func TestRegisterTLSUsesPinnedCAWithoutHostnameVerification(t *testing.T) {
	ca, caKey, cfg := registerTestCA(t, tlsModeVerifyCA, testDBAddr)
	require.True(t, cfg.InsecureSkipVerify)
	require.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	require.Empty(t, cfg.ServerName)
	// The trusted pool lives inside the VerifyConnection closure, not on the
	// config: VERIFY_CA never sets RootCAs directly.
	require.Nil(t, cfg.RootCAs)
	require.Nil(t, cfg.VerifyPeerCertificate)
	require.NotNil(t, cfg.VerifyConnection)

	// Steady state: a chain actually signed by the pinned CA verifies.
	require.NoError(t, cfg.VerifyConnection(tls.ConnectionState{
		PeerCertificates: testLeafChain(t, ca, caKey),
	}))
}

func TestVerifyCARejectsForgedCAWithCopiedSubjectDifferentKey(t *testing.T) {
	_, _, cfg := registerTestCA(t, tlsModeVerifyCA, testDBAddr)

	// The bypass the old rotatingTrust/adopt machinery made possible: an
	// attacker cannot know the pinned CA's private key, but the subject DN is
	// public, so they mint their own CA carrying the identical subject and
	// self-sign it for free. There is no adoption path left to fall into; the
	// chain simply fails to verify against the one pinned root.
	forgedCA, forgedKey := testNamedCA(t, "MySQL_Endpoint_CA")
	err := cfg.VerifyConnection(tls.ConnectionState{
		PeerCertificates: testLeafChainWithCA(t, forgedCA, forgedKey),
	})
	require.ErrorContains(t, err, "does not chain to the pinned DB_CA_CERT")
	require.ErrorContains(t, err, "OCI console")
	require.ErrorContains(t, err, "Doppler")
}

func TestVerifyCARejectsCAReusingPinnedKeyButNotSigningLeaf(t *testing.T) {
	_, caKey, cfg := registerTestCA(t, tlsModeVerifyCA, testDBAddr)

	// The presented chain carries a CA reusing the pinned key as its trailing
	// entry, but the leaf was actually signed by an unrelated CA. With no
	// promotion logic left, this is rejected the same way any other
	// unrecognised chain is: there is nothing special-cased about the pinned
	// key showing up somewhere in the chain.
	leafCA, leafKey, _ := testCA(t)
	bystanderCA := selfSignWithKey(t, "MySQL_Endpoint_CA", caKey, 3)
	chain := append(testLeafChain(t, leafCA, leafKey), bystanderCA)
	err := cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: chain})
	require.ErrorContains(t, err, "does not chain to the pinned DB_CA_CERT")
}

func TestVerifyCARejectsUnrelatedCA(t *testing.T) {
	_, _, cfg := registerTestCA(t, tlsModeVerifyCA, testDBAddr)

	untrustedCA, untrustedKey, _ := testCA(t)
	err := cfg.VerifyConnection(tls.ConnectionState{
		PeerCertificates: testLeafChain(t, untrustedCA, untrustedKey),
	})
	require.ErrorContains(t, err, "does not chain to the pinned DB_CA_CERT")
}

func TestVerifyCARejectsEmptyPeerChain(t *testing.T) {
	_, _, cfg := registerTestCA(t, tlsModeVerifyCA, testDBAddr)
	require.ErrorContains(t, cfg.VerifyConnection(tls.ConnectionState{}), "server presented no certificate")
}

// --- VERIFY_IDENTITY: the target mode once the DB System is switched to a
// BYOC certificate carrying a SAN for its endpoint. ---

func TestRegisterTLSVerifyIdentityBuildsStandardConfig(t *testing.T) {
	addr := "db.internal.example:3306"
	_, _, cfg := registerTestCA(t, tlsModeVerifyIdentity, addr)

	require.False(t, cfg.InsecureSkipVerify)
	require.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	require.Equal(t, "db.internal.example", cfg.ServerName)
	require.NotNil(t, cfg.RootCAs)
	// No custom hook: verification is Go's standard RootCAs + ServerName check.
	require.Nil(t, cfg.VerifyPeerCertificate)
	require.Nil(t, cfg.VerifyConnection)
}

func TestRegisterTLSVerifyIdentityRejectsAddrWithoutPort(t *testing.T) {
	_, _, caPEM := testCA(t)
	_, err := registerTLS(caPEM, tlsModeVerifyIdentity, "db.internal.example")
	require.ErrorContains(t, err, "must be host:port")
}

func TestVerifyIdentityAcceptsChainAndHostnameTogether(t *testing.T) {
	addr := "db.internal.example:3306"
	ca, caKey, cfg := registerTestCA(t, tlsModeVerifyIdentity, addr)

	// registerTLS does not install a custom verifier for this mode; it relies
	// on crypto/tls running the standard checks against RootCAs/ServerName at
	// handshake time. Exercise those same primitives directly here to prove
	// the config actually enables both the chain and the hostname check.
	leaf := testLeafWithSAN(t, ca, caKey, "db.internal.example")
	_, err := leaf.Verify(x509.VerifyOptions{Roots: cfg.RootCAs, DNSName: cfg.ServerName})
	require.NoError(t, err)
}

func TestVerifyIdentityRejectsHostnameMismatch(t *testing.T) {
	addr := "db.internal.example:3306"
	ca, caKey, cfg := registerTestCA(t, tlsModeVerifyIdentity, addr)

	leaf := testLeafWithSAN(t, ca, caKey, "someone-else.example")
	_, err := leaf.Verify(x509.VerifyOptions{Roots: cfg.RootCAs, DNSName: cfg.ServerName})
	require.Error(t, err)
}

func TestVerifyIdentityRejectsUnrelatedCA(t *testing.T) {
	addr := "db.internal.example:3306"
	_, _, cfg := registerTestCA(t, tlsModeVerifyIdentity, addr)

	untrustedCA, untrustedKey := testNamedCA(t, "Unrelated_CA")
	leaf := testLeafWithSAN(t, untrustedCA, untrustedKey, "db.internal.example")
	_, err := leaf.Verify(x509.VerifyOptions{Roots: cfg.RootCAs, DNSName: cfg.ServerName})
	require.Error(t, err)
}

// --- Pinned CA expiry warning. ---

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

// captureGlobalLogs swaps zap's global logger (what zap.L() resolves to, the
// same global pkg/logger.New wires up via zap.ReplaceGlobals in every real
// service) for an observed logger, and restores the previous global on
// cleanup.
func captureGlobalLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()

	core, logs := observer.New(zap.ErrorLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(restore)

	return logs
}

// registerTestCA pins a fresh test CA in the driver's TLS registry (with
// cleanup) and returns the CA pair plus the registered config, so each test
// starts from the same known trust state.
func registerTestCA(t *testing.T, mode tlsMode, addr string) (*x509.Certificate, *ecdsa.PrivateKey, *tls.Config) {
	t.Helper()
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	ca, caKey, caPEM := testCA(t)
	_, err := registerTLS(caPEM, mode, addr)
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

// testNamedCAWithValidity builds a self-signed CA with a caller-chosen
// notAfter, used to drive the expiry warning across its threshold.
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

// selfSignWithKey mints a self-signed CA certificate over an EXISTING key
// pair rather than a fresh one, standing in for a chain entry that reuses the
// pinned CA's key without being the actual issuer of the presented leaf.
func selfSignWithKey(t *testing.T, commonName string, key *ecdsa.PrivateKey, serial int64) *x509.Certificate {
	t.Helper()

	template := x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{CommonName: commonName},
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
	return cert
}

// testLeafChainWithCA mirrors what the managed endpoint actually presents:
// the server certificate followed by the CA that signed it.
func testLeafChainWithCA(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey) []*x509.Certificate {
	t.Helper()

	return append(testLeafChain(t, ca, caKey), ca)
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

// testLeafWithSAN mints a leaf certificate carrying a DNS SAN, standing in
// for a BYOC certificate issued through the OCI Certificates Service: unlike
// the service-defined certificate, it has a name VERIFY_IDENTITY can check.
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
