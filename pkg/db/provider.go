package db

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"entgo.io/ent/dialect"

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

	tlsName, err := registerTLS([]byte(os.Getenv("DB_CA_CERT")))
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
// When DB_CA_CERT holds the endpoint CA (PEM), the server certificate is
// verified against it. The HeatWave endpoint certificate carries no SAN and we
// dial it by private IP, so Go's default identity check cannot apply; instead
// we pin the CA and verify the presented chain back to it ourselves, which
// authenticates the server (MITM-resistant) without a hostname match. Without a
// CA we still negotiate TLS so the wire stays encrypted, just unauthenticated.
func registerTLS(caPEM []byte) (string, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	caPEM = []byte(strings.TrimSpace(string(caPEM)))

	if len(caPEM) > 0 {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			return "", fmt.Errorf("db: DB_CA_CERT did not contain a valid PEM certificate")
		}
		cfg.RootCAs = roots
		cfg.InsecureSkipVerify = true // we verify the chain ourselves below, minus the (absent) hostname identity
		cfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			certs := make([]*x509.Certificate, 0, len(rawCerts))
			for _, raw := range rawCerts {
				c, err := x509.ParseCertificate(raw)
				if err != nil {
					return fmt.Errorf("db: parse server certificate: %w", err)
				}
				certs = append(certs, c)
			}
			if len(certs) == 0 {
				return fmt.Errorf("db: server presented no certificate")
			}
			intermediates := x509.NewCertPool()
			for _, c := range certs[1:] {
				intermediates.AddCert(c)
			}
			_, err := certs[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates})
			return err
		}
	} else {
		cfg.InsecureSkipVerify = true // encrypt even without a pinned CA
	}

	if err := mysql.RegisterTLSConfig(tlsConfigName, cfg); err != nil {
		return "", err
	}
	return tlsConfigName, nil
}
