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
	// The trusted pool lives inside the VerifyConnection closure (it can be
	// swapped on CA rotation), not on the config.
	require.Nil(t, cfg.RootCAs)
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

func TestVerifyConnectionAdoptsRotatedCA(t *testing.T) {
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	oldCA, oldKey, oldCAPEM := testCA(t)
	_, err := registerTLS(oldCAPEM)
	require.NoError(t, err)

	cfg := registeredTLSConfig(t, "10.0.0.4:3306")

	// A rotated CA with the pinned subject, presented by the server itself,
	// is adopted and the handshake that saw it succeeds.
	newCA, newKey, _ := testCA(t)
	rotated := testLeafChainWithCA(t, newCA, newKey)
	require.NoError(t, cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: rotated}))

	// Adoption replaced the pool: chains from the pre-rotation CA are no
	// longer trusted, and the rotated chain keeps verifying.
	require.Error(t, cfg.VerifyConnection(tls.ConnectionState{
		PeerCertificates: testLeafChain(t, oldCA, oldKey),
	}))
	require.NoError(t, cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: rotated}))
}

func TestVerifyConnectionRejectsRotationWithDifferentSubject(t *testing.T) {
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	_, _, caPEM := testCA(t)
	_, err := registerTLS(caPEM)
	require.NoError(t, err)

	cfg := registeredTLSConfig(t, "10.0.0.4:3306")

	imposterCA, imposterKey := testNamedCA(t, "Imposter_CA")
	require.ErrorContains(t, cfg.VerifyConnection(tls.ConnectionState{
		PeerCertificates: testLeafChainWithCA(t, imposterCA, imposterKey),
	}), "no rotation candidate")
}

func TestVerifyConnectionRejectsRotationWithoutPresentedCA(t *testing.T) {
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	_, _, caPEM := testCA(t)
	_, err := registerTLS(caPEM)
	require.NoError(t, err)

	cfg := registeredTLSConfig(t, "10.0.0.4:3306")

	// Leaf-only chain from an untrusted CA: nothing to adopt, fail closed.
	untrustedCA, untrustedKey, _ := testCA(t)
	require.ErrorContains(t, cfg.VerifyConnection(tls.ConnectionState{
		PeerCertificates: testLeafChain(t, untrustedCA, untrustedKey),
	}), "no rotation candidate")
}

func TestVerifyConnectionRejectsRotatedCANotSigningLeaf(t *testing.T) {
	t.Cleanup(func() { mysql.DeregisterTLSConfig(tlsConfigName) })

	_, _, caPEM := testCA(t)
	_, err := registerTLS(caPEM)
	require.NoError(t, err)

	cfg := registeredTLSConfig(t, "10.0.0.4:3306")

	// The presented CA carries the pinned subject but did not sign the leaf.
	leafCA, leafKey, _ := testCA(t)
	bystanderCA, _, _ := testCA(t)
	chain := append(testLeafChain(t, leafCA, leafKey), bystanderCA)
	require.ErrorContains(t, cfg.VerifyConnection(tls.ConnectionState{
		PeerCertificates: chain,
	}), "does not sign the server certificate")
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

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	return issueTestCertificate(t, template, nil, nil)
}

// testLeafChainWithCA mirrors what the managed endpoint actually presents
// after a rotation: the server certificate followed by the CA that signed it.
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
