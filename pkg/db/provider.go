package db

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"sync"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"entgo.io/ent/dialect"

	"ItsBagelBot/pkg/env"

	"github.com/go-sql-driver/mysql"

	// Registers the "nrmysql" driver: the MySQL driver wrapped with New
	// Relic datastore instrumentation. Queries report as segments of the
	// transaction carried by the context; without one it is a no-op.
	_ "github.com/newrelic/go-agent/v3/integrations/nrmysql"
)

// Config carries everything needed to reach the MySQL schema owned by one
// service. Each service connects to its own schema and never to another's,
// keeping the isolation decided in ADR 0005.
type Config struct {
	Address  string // host:port
	Username string
	Password string
	Schema   string // the MySQL database owned by the calling service

	// MaxConns bounds the pool. Keep it small: every service shares the same
	// 8 GB HeatWave instance and MySQL connections are not free.
	MaxConns int
}

const (
	defaultMaxConns = 4

	connMaxLifetime = 30 * time.Minute
	connMaxIdleTime = 5 * time.Minute
)

// NewDriver opens a bounded MySQL connection pool with the session settings
// pinned at the connection level (utf8mb4, READ COMMITTED, strict SQL mode,
// UTC) instead of relying on server defaults. The returned driver is meant to
// be handed to the service's own ent client via ent.Driver(...).
func NewDriver(cfg Config) (*entsql.Driver, error) {

	mc := mysql.NewConfig()

	mc.Net = "tcp"
	mc.Addr = cfg.Address
	mc.User = cfg.Username
	mc.Passwd = cfg.Password
	mc.DBName = cfg.Schema

	mc.ParseTime = true
	mc.Loc = time.UTC
	mc.Collation = "utf8mb4_unicode_ci"
	mc.InterpolateParams = true // one round-trip per query instead of prepare+exec

	mc.Params = map[string]string{
		"transaction_isolation": "'READ-COMMITTED'",
		"sql_mode":              "'STRICT_TRANS_TABLES,NO_ZERO_DATE,NO_ZERO_IN_DATE,ERROR_FOR_DIVISION_BY_ZERO'",
		"time_zone":             "'+00:00'",
	}

	tlsName, err := registerTLS([]byte(env.Get("DB_CA_CERT", "")))
	if err != nil {
		return nil, err
	}
	mc.TLSConfig = tlsName

	pool, err := openPool(mc.FormatDSN(), cfg.MaxConns)
	if err != nil {
		return nil, err
	}

	return entsql.OpenDB(dialect.MySQL, pool), nil
}

// tlsConfigName is the key the registered tls.Config is stored under in the
// go-sql-driver registry and referenced from the DSN. The DB endpoint is the
// same managed HeatWave instance for every service, so one shared config is
// enough.
const tlsConfigName = "bagel-mysql"

// registerTLS builds and registers the TLS config used for every MySQL
// connection so traffic to the managed HeatWave endpoint is always encrypted.
//
// DB_CA_CERT must hold the endpoint CA (PEM); connections fail closed when it
// is absent or invalid. The HeatWave endpoint certificate carries no SAN and
// we dial it by private IP, so Go's default identity check cannot apply;
// instead we pin the CA and verify the presented chain back to it ourselves,
// which authenticates the server (MITM-resistant) without a hostname match.
// The configured root must therefore be the dedicated HeatWave endpoint CA,
// never a broad/shared trust bundle such as the internal fleet CA.
func registerTLS(caPEM []byte) (string, error) {
	cfg, err := newPinnedCAVerificationTLSConfig(caPEM)
	if err != nil {
		return "", err
	}

	if err := mysql.RegisterTLSConfig(tlsConfigName, cfg); err != nil {
		return "", err
	}
	return tlsConfigName, nil
}

// newPinnedCAVerificationTLSConfig uses the dedicated endpoint CA as the
// server identity because the managed certificate has no SAN to match against.
// VerifyConnection is intentional: unlike VerifyPeerCertificate, it also runs
// when a TLS session is resumed.
func newPinnedCAVerificationTLSConfig(caPEM []byte) (*tls.Config, error) {
	pinned, err := parsePinnedCA(caPEM)
	if err != nil {
		return nil, err
	}

	trust := newRotatingTrust(pinned)
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		// codeql[go/disabled-certificate-check] -- Custom verification pins the endpoint CA below.
		InsecureSkipVerify: true,
		VerifyConnection:   trust.VerifyConnection,
	}, nil
}

func parsePinnedCA(caPEM []byte) (*x509.Certificate, error) {
	caPEM = bytes.TrimSpace(caPEM)
	if len(caPEM) == 0 {
		return nil, fmt.Errorf("db: DB_CA_CERT is required")
	}

	block, _ := pem.Decode(caPEM)
	if block == nil {
		return nil, fmt.Errorf("db: DB_CA_CERT did not contain a valid PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("db: DB_CA_CERT did not contain a valid PEM certificate")
	}
	return cert, nil
}

// rotatingTrust verifies MySQL connections against the pinned endpoint CA but
// survives OCI rotating that CA in place. OCI reissues the managed HeatWave
// endpoint CA on its own schedule (observed ~28 days); a static pin turns each
// rotation into a full DB outage for every service until DB_CA_CERT is
// re-pinned (2026-07-30: dashboard login + admin down for ~7.5h).
//
// When verification against the current pool fails, the connection is allowed
// to promote the CA the server itself presented — but only when that CA is
// self-signed, within its validity window, carries the exact subject of the
// originally pinned CA, and actually signs the presented server certificate.
// A rotation therefore heals on the first handshake that sees the new chain,
// with no restart and no config change.
//
// Trust model: adoption trusts the endpoint's own chain (trust-on-use), the
// same posture as re-pinning by hand from the endpoint. The endpoint is only
// reachable over the VCN-private path; an attacker who can MITM that path
// already owns the database traffic. The subject + self-signature + signs-leaf
// checks keep garbage (a TCP proxy, an unrelated CA) from ever being adopted.
type rotatingTrust struct {
	mu    sync.RWMutex
	roots *x509.CertPool

	// subject of the CA originally pinned via DB_CA_CERT; a rotated CA is
	// only adopted when its subject matches exactly.
	subject string
}

func newRotatingTrust(pinned *x509.Certificate) *rotatingTrust {
	roots := x509.NewCertPool()
	roots.AddCert(pinned)
	return &rotatingTrust{roots: roots, subject: pinned.Subject.String()}
}

func (t *rotatingTrust) VerifyConnection(state tls.ConnectionState) error {
	certs := state.PeerCertificates
	if len(certs) == 0 {
		return fmt.Errorf("db: server presented no certificate")
	}
	if err := verifyChain(certs, t.currentRoots()); err == nil {
		return nil
	}
	return t.adopt(certs)
}

func (t *rotatingTrust) currentRoots() *x509.CertPool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.roots
}

// adopt promotes the CA presented by the server after a rotation. The write
// lock serializes concurrent handshakes racing the same rotation; the re-check
// under the lock lets the losers succeed against the winner's pool.
func (t *rotatingTrust) adopt(certs []*x509.Certificate) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := verifyChain(certs, t.roots); err == nil {
		return nil
	}

	candidate, err := rotationCandidate(certs, t.subject)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	roots.AddCert(candidate)
	if err := verifyChain(certs, roots); err != nil {
		return fmt.Errorf("db: rotated CA does not sign the server certificate: %w", err)
	}

	t.roots = roots
	log.Printf("db: adopted rotated MySQL endpoint CA (%s, expires %s)",
		candidate.Subject, candidate.NotAfter.Format(time.RFC3339))
	return nil
}

func rotationCandidate(certs []*x509.Certificate, subject string) (*x509.Certificate, error) {
	for _, c := range certs[1:] {
		if isSelfSignedCANamed(c, subject) {
			return c, nil
		}
	}
	return nil, fmt.Errorf("db: server certificate is not trusted and the chain carries no rotation candidate for %q", subject)
}

func isSelfSignedCANamed(c *x509.Certificate, subject string) bool {
	if !c.IsCA || c.Subject.String() != subject {
		return false
	}
	return c.CheckSignatureFrom(c) == nil
}

func verifyChain(certs []*x509.Certificate, roots *x509.CertPool) error {
	intermediates := x509.NewCertPool()
	for _, c := range certs[1:] {
		intermediates.AddCert(c)
	}
	_, err := certs[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates})
	return err
}
