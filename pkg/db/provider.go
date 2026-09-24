// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"entgo.io/ent/dialect"

	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/tlsenv"

	"github.com/go-sql-driver/mysql"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

type Config struct {
	Address  string
	Username string
	Password string
	Schema   string

	MaxConns int

	Monitor *newrelic.Application
}

const (
	// DB pods x this must stay under HeatWave's max_connections (200).
	defaultMaxConns = 8

	// Plus connMaxLifetimeJitter, must stay under the server's wait_timeout (8h).
	connMaxLifetime = 30 * time.Minute

	connMaxLifetimeJitter = 10 * time.Minute

	connMaxIdleTime = 0

	dialTimeout  = 10 * time.Second
	readTimeout  = 30 * time.Second
	writeTimeout = 30 * time.Second
)

func NewDriver(cfg Config) (*entsql.Driver, error) {

	mc := newMySQLConfig(cfg)

	mode, err := resolveTLSMode()
	if err != nil {
		return nil, err
	}
	tlsName, err := registerTLS([]byte(env.Get("DB_CA_CERT", "")), mode, cfg.Address)
	if err != nil {
		return nil, err
	}
	mc.TLSConfig = tlsName

	pool, err := openPool(mc, cfg)
	if err != nil {
		return nil, err
	}

	return entsql.OpenDB(dialect.MySQL, pool), nil
}

func newMySQLConfig(cfg Config) *mysql.Config {
	mc := mysql.NewConfig()

	mc.Net = "tcp"
	mc.Addr = cfg.Address
	mc.User = cfg.Username
	mc.Passwd = cfg.Password
	mc.DBName = cfg.Schema

	mc.ParseTime = true
	mc.Loc = time.UTC
	mc.Collation = "utf8mb4_unicode_ci"
	mc.InterpolateParams = true

	mc.Timeout = dialTimeout
	// Never remove: the NLB drops idle flows without a RST, so a query would block forever.
	mc.ReadTimeout = readTimeout
	mc.WriteTimeout = writeTimeout

	mc.Params = map[string]string{
		"transaction_isolation": "'READ-COMMITTED'",
		"sql_mode":              "'STRICT_TRANS_TABLES,NO_ZERO_DATE,NO_ZERO_IN_DATE,ERROR_FOR_DIVISION_BY_ZERO'",
		"time_zone":             "'+00:00'",
	}

	return mc
}

const tlsConfigName = "bagel-mysql"

const tlsSessionCacheSize = 64

var mysqlSessionCache = tls.NewLRUClientSessionCache(tlsSessionCacheSize)

// Re-pin DB_CA_CERT by hand; never adopt a CA the server presents.
type tlsMode string

const (
	tlsModeVerifyCA tlsMode = "VERIFY_CA"

	tlsModeVerifyIdentity tlsMode = "VERIFY_IDENTITY"

	tlsModeEnvVar = "DB_TLS_MODE"
)

func resolveTLSMode() (tlsMode, error) {
	switch mode := tlsMode(env.Get(tlsModeEnvVar, string(tlsModeVerifyCA))); mode {
	case tlsModeVerifyCA, tlsModeVerifyIdentity:
		return mode, nil
	default:
		return "", fmt.Errorf("db: %s must be %q or %q, got %q",
			tlsModeEnvVar, tlsModeVerifyCA, tlsModeVerifyIdentity, mode)
	}
}

func registerTLS(caPEM []byte, mode tlsMode, addr string) (string, error) {
	cfg, err := newMySQLTLSConfig(caPEM, mode, addr)
	if err != nil {
		return "", err
	}

	if err := mysql.RegisterTLSConfig(tlsConfigName, cfg); err != nil {
		return "", err
	}
	return tlsConfigName, nil
}

func newMySQLTLSConfig(caPEM []byte, mode tlsMode, addr string) (*tls.Config, error) {
	pinned, err := parsePinnedCA(caPEM)
	if err != nil {
		return nil, err
	}
	warnIfPinnedCANearExpiry(pinned)

	clientCerts, err := loadClientKeyPair()
	if err != nil {
		return nil, err
	}

	if mode != tlsModeVerifyIdentity {
		cfg := verifyCATLSConfig(pinned)
		cfg.Certificates = clientCerts
		return cfg, nil
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("db: DB_ADDR must be host:port for VERIFY_IDENTITY, got %q: %w", addr, err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(pinned)
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		RootCAs:            roots,
		ServerName:         host,
		Certificates:       clientCerts,
		ClientSessionCache: mysqlSessionCache,
	}, nil
}

const (
	clientCertEnvVar = tlsenv.Var("DB_CLIENT_CERT")
	clientKeyEnvVar  = tlsenv.Var("DB_CLIENT_KEY")
)

func loadClientKeyPair() ([]tls.Certificate, error) {

	pair, err := tlsenv.PairFromEnv(clientCertEnvVar, clientKeyEnvVar)
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	if !pair.Configured() {
		return nil, nil
	}

	certPEM := strings.TrimSpace(env.Get(string(clientCertEnvVar), ""))
	keyPEM := strings.TrimSpace(env.Get(string(clientKeyEnvVar), ""))
	keyPair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("db: parsing %s/%s client key pair: %w",
			clientCertEnvVar, clientKeyEnvVar, err)
	}
	return []tls.Certificate{keyPair}, nil
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

const caExpiryWarningWindow = 30 * 24 * time.Hour

func warnIfPinnedCANearExpiry(pinned *x509.Certificate) {
	remaining := time.Until(pinned.NotAfter)
	if remaining > caExpiryWarningWindow {
		return
	}
	zap.L().Error("db: pinned MySQL endpoint CA (DB_CA_CERT) is close to expiry, "+
		"re-fetch the current CA (OCI console for the service-defined CA, or the "+
		"OCI Certificates Service certificate for a BYOC CA) and update DB_CA_CERT in Doppler",
		zap.Time("notAfter", pinned.NotAfter),
		zap.Duration("remaining", remaining),
	)
}

func verifyCATLSConfig(pinned *x509.Certificate) *tls.Config {
	roots := x509.NewCertPool()
	roots.AddCert(pinned)

	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		// codeql[go/disabled-certificate-check] -- VERIFY_CA: the endpoint cert has no SAN.
		InsecureSkipVerify: true,
		VerifyConnection:   verifyAgainstPinned(roots),
		ClientSessionCache: mysqlSessionCache,
	}
}

func verifyAgainstPinned(roots *x509.CertPool) func(tls.ConnectionState) error {
	return func(state tls.ConnectionState) error {
		certs := state.PeerCertificates
		if len(certs) == 0 {
			return fmt.Errorf("db: server presented no certificate")
		}
		if err := verifyChain(certs, roots); err != nil {
			return fmt.Errorf("db: server certificate does not chain to the pinned "+
				"DB_CA_CERT, re-fetch the endpoint CA from the OCI console and update "+
				"DB_CA_CERT in Doppler: %w", err)
		}
		return nil
	}
}

func verifyChain(certs []*x509.Certificate, roots *x509.CertPool) error {
	intermediates := x509.NewCertPool()
	for _, c := range certs[1:] {
		intermediates.AddCert(c)
	}
	_, err := certs[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates})
	return err
}
